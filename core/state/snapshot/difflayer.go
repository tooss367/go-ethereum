// Copyright 2019 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package snapshot

import (
	"bytes"
	"fmt"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// diffLayer represents a collection of modifications made to a state snapshot
// after running a block on top. It contains one sorted list for the account trie
// and one-one list for each storage tries.
//
// The goal of a diff layer is to act as a journal, tracking recent modifications
// made to the state, that have not yet graduated into a semi-immutable state.
type diffLayer struct {
	parent snapshot // Parent snapshot modified by this one, never nil
	memory uint64   // Approximate guess as to how much memory we use

	root  common.Hash // Root hash to which this snapshot diff belongs to
	stale bool        // Signals that the layer became stale (state progressed)

	accountList []common.Hash                          // List of account for iteration. If it exists, it's sorted, otherwise it's nil
	accountData map[common.Hash][]byte                 // Keyed accounts for direct retrival (nil means deleted)
	storageList map[common.Hash][]common.Hash          // List of storage slots for iterated retrievals, one per account. Any existing lists are sorted if non-nil
	storageData map[common.Hash]map[common.Hash][]byte // Keyed storage slots for direct retrival. one per account (nil means deleted)

	lock sync.RWMutex
}

// newDiffLayer creates a new diff on top of an existing snapshot, whether that's a low
// level persistent database or a hierarchical diff already.
func newDiffLayer(parent snapshot, root common.Hash, accounts map[common.Hash][]byte, storage map[common.Hash]map[common.Hash][]byte) *diffLayer {
	// Create the new layer with some pre-allocated data segments
	dl := &diffLayer{
		parent:      parent,
		root:        root,
		accountData: accounts,
		storageData: storage,
	}
	// Determine mem size
	for _, data := range accounts {
		dl.memory += uint64(len(data))
	}
	// Fill the storage hashes and sort them for the iterator
	dl.storageList = make(map[common.Hash][]common.Hash)

	for accountHash, slots := range storage {
		// If the slots are nil, sanity check that it's a deleted account
		if slots == nil {
			// Ensure that the account was just marked as deleted
			if account, ok := accounts[accountHash]; account != nil || !ok {
				panic(fmt.Sprintf("storage in %#x nil, but account conflicts (%#x, exists: %v)", accountHash, account, ok))
			}
			// Everything ok, store the deletion mark and continue
			dl.storageList[accountHash] = nil
			continue
		}
		// Storage slots are not nil so entire contract was not deleted, ensure the
		// account was just updated.
		if account, ok := accounts[accountHash]; account == nil || !ok {
			log.Error(fmt.Sprintf("storage in %#x exists, but account nil (exists: %v)", accountHash, ok))
		}
		// Determine mem size
		for _, data := range slots {
			dl.memory += uint64(len(data))
		}
	}
	dl.memory += uint64(len(dl.storageList) * common.HashLength)

	return dl
}

// Root returns the root hash for which this snapshot was made.
func (dl *diffLayer) Root() common.Hash {
	return dl.root
}

// Stale return whether this layer has become stale (was flattened across) or if
// it's still live.
func (dl *diffLayer) Stale() bool {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	return dl.stale
}

// Account directly retrieves the account associated with a particular hash in
// the snapshot slim data format.
func (dl *diffLayer) Account(hash common.Hash) (*Account, error) {
	data, err := dl.AccountRLP(hash)
	if err != nil {
		return nil, err
	}
	if len(data) == 0 { // can be both nil and []byte{}
		return nil, nil
	}
	account := new(Account)
	if err := rlp.DecodeBytes(data, account); err != nil {
		panic(err)
	}
	return account, nil
}

// AccountRLP directly retrieves the account RLP associated with a particular
// hash in the snapshot slim data format.
func (dl *diffLayer) AccountRLP(hash common.Hash) ([]byte, error) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	// If the layer was flattened into, consider it invalid (any live reference to
	// the original should be marked as unusable).
	if dl.stale {
		return nil, ErrSnapshotStale
	}
	// If the account is known locally, return it. Note, a nil account means it was
	// deleted, and is a different notion than an unknown account!
	if data, ok := dl.accountData[hash]; ok {
		return data, nil
	}
	// Account unknown to this diff, resolve from parent
	return dl.parent.AccountRLP(hash)
}

// Storage directly retrieves the storage data associated with a particular hash,
// within a particular account. If the slot is unknown to this diff, it's parent
// is consulted.
func (dl *diffLayer) Storage(accountHash, storageHash common.Hash) ([]byte, error) {
	dl.lock.RLock()
	defer dl.lock.RUnlock()

	// If the layer was flattened into, consider it invalid (any live reference to
	// the original should be marked as unusable).
	if dl.stale {
		return nil, ErrSnapshotStale
	}
	// If the account is known locally, try to resolve the slot locally. Note, a nil
	// account means it was deleted, and is a different notion than an unknown account!
	if storage, ok := dl.storageData[accountHash]; ok {
		if storage == nil {
			return nil, nil
		}
		if data, ok := storage[storageHash]; ok {
			return data, nil
		}
	}
	// Account - or slot within - unknown to this diff, resolve from parent
	return dl.parent.Storage(accountHash, storageHash)
}

// Update creates a new layer on top of the existing snapshot diff tree with
// the specified data items.
func (dl *diffLayer) Update(blockRoot common.Hash, accounts map[common.Hash][]byte, storage map[common.Hash]map[common.Hash][]byte) *diffLayer {
	return newDiffLayer(dl, blockRoot, accounts, storage)
}

// flatten pushes all data from this point downwards, flattening everything into
// a single diff at the bottom. Since usually the lowermost diff is the largest,
// the flattening bulds up from there in reverse.
func (dl *diffLayer) flatten() snapshot {
	// If the parent is not diff, we're the first in line, return unmodified
	parent, ok := dl.parent.(*diffLayer)
	if !ok {
		return dl
	}
	// Parent is a diff, flatten it first (note, apart from weird corned cases,
	// flatten will realistically only ever merge 1 layer, so there's no need to
	// be smarter about grouping flattens together).
	parent = parent.flatten().(*diffLayer)

	parent.lock.Lock()
	defer parent.lock.Unlock()

	// Before actually writing all our data to the parent, first ensure that the
	// parent hasn't been 'corrupted' by someone else already flattening into it
	if parent.stale {
		panic("parent diff layer is stale") // we've flattened into the same parent from two children, boo
	}
	parent.stale = true

	// Overwrite all the updated accounts blindly, merge the sorted list
	for hash, data := range dl.accountData {
		parent.accountData[hash] = data
	}
	// Overwrite all the updates storage slots (individually)
	for accountHash, storage := range dl.storageData {
		// If storage didn't exist (or was deleted) in the parent; or if the storage
		// was freshly deleted in the child, overwrite blindly
		if parent.storageData[accountHash] == nil || storage == nil {
			parent.storageData[accountHash] = storage
			continue
		}
		// Storage exists in both parent and child, merge the slots
		comboData := parent.storageData[accountHash]
		for storageHash, data := range storage {
			comboData[storageHash] = data
		}
		parent.storageData[accountHash] = comboData
	}
	// Return the combo parent
	return &diffLayer{
		parent:      parent.parent,
		root:        dl.root,
		storageList: parent.storageList,
		storageData: parent.storageData,
		accountList: parent.accountList,
		accountData: parent.accountData,
		memory:      parent.memory + dl.memory,
	}
}

// Journal commits an entire diff hierarchy to disk into a single journal file.
// This is meant to be used during shutdown to persist the snapshot without
// flattening everything down (bad for reorgs).
func (dl *diffLayer) Journal() error {
	writer, err := dl.journal()
	if err != nil {
		return err
	}
	writer.Close()
	return nil
}

// AccountList returns a sorted list of all accounts in this difflayer.
func (dl *diffLayer) AccountList() []common.Hash {
	dl.lock.Lock()
	defer dl.lock.Unlock()
	if dl.accountList != nil {
		return dl.accountList
	}
	accountList := make([]common.Hash, len(dl.accountData))
	i := 0
	for k, _ := range dl.accountData {
		accountList[i] = k
		i++
		// This would be a pretty good opportunity to also
		// calculate the size, if we want to
	}
	sort.Sort(hashes(accountList))
	dl.accountList = accountList
	return dl.accountList
}

// StorageList returns a sorted list of all storage slot hashes
// in this difflayer for the given account.
func (dl *diffLayer) StorageList(accountHash common.Hash) []common.Hash {
	dl.lock.Lock()
	defer dl.lock.Unlock()
	if dl.storageList[accountHash] != nil {
		return dl.storageList[accountHash]
	}
	accountStorageMap := dl.storageData[accountHash]
	accountStorageList := make([]common.Hash, len(accountStorageMap))
	i := 0
	for k, _ := range accountStorageMap {
		accountStorageList[i] = k
		i++
		// This would be a pretty good opportunity to also
		// calculate the size, if we want to
	}
	sort.Sort(hashes(accountStorageList))
	dl.storageList[accountHash] = accountStorageList
	return accountStorageList
}

type Iterator interface {
	Next() bool
	Key() common.Hash
}

func (dl *diffLayer) newIterator() Iterator {
	dl.AccountList()
	return &dlIterator{dl, -1}
}

type dlIterator struct {
	layer *diffLayer
	index int
}

func (it *dlIterator) Next() bool {
	if it.index < len(it.layer.accountList) {
		it.index++
	}
	return it.index < len(it.layer.accountList)
}

func (it *dlIterator) Key() common.Hash {
	if it.index < len(it.layer.accountList) {
		return it.layer.accountList[it.index]
	}
	return common.Hash{}
}

type binaryIterator struct {
	a     Iterator
	b     Iterator
	aDone bool
	bDone bool
	k     common.Hash
}

func (dl *diffLayer) newBinaryIterator() Iterator {
	parent, ok := dl.parent.(*diffLayer)
	if !ok {
		// parent is the disk layer
		return dl.newIterator()
	}
	l := &binaryIterator{
		a: dl.newIterator(),
		b: parent.newBinaryIterator()}

	l.aDone = !l.a.Next()
	l.bDone = !l.b.Next()
	return l
}

func (it *binaryIterator) Next() bool {

	if it.aDone && it.bDone {
		return false
	}
	nextB := it.b.Key()
first:
	nextA := it.a.Key()
	if it.aDone {
		it.bDone = !it.b.Next()
		it.k = nextB
		return true
	}
	if it.bDone {
		it.aDone = !it.a.Next()
		it.k = nextA
		return true
	}
	if diff := bytes.Compare(nextA[:], nextB[:]); diff < 0 {
		it.aDone = !it.a.Next()
		it.k = nextA
		return true
	} else if diff == 0 {
		// Now we need to advance one of them
		it.aDone = !it.a.Next()
		goto first
	}
	it.bDone = !it.b.Next()
	it.k = nextB
	return true
}

func (it *binaryIterator) Key() common.Hash {
	return it.k
}

func (dl *diffLayer) iterators() []Iterator {
	if parent, ok := dl.parent.(*diffLayer); ok {
		iterators := parent.iterators()
		return append(iterators, dl.newIterator())
	}
	return []Iterator{dl.newIterator()}
}

// fastIterator is a more optimized multi-layer iterator which maintains a
// direct mapping of all iterators leading down to the bottom layer
type fastIterator struct {
	iterators []Iterator
	initiated bool
}

// Len returns the number of active iterators
func (fi *fastIterator) Len() int {
	return len(fi.iterators)
}

// Less implements sort.Interface
func (fi *fastIterator) Less(i, j int) bool {
	a := fi.iterators[i].Key()
	b := fi.iterators[j].Key()
	return bytes.Compare(a[:], b[:]) < 0
}

// Swap implements sort.Interface
func (fi *fastIterator) Swap(i, j int) {
	fi.iterators[i], fi.iterators[j] = fi.iterators[j], fi.iterators[i]
}

// Next implements the Iterator interface. It returns false if no more elemnts
// can be retrieved (false == exhausted)
func (fi *fastIterator) Next() bool {
	if len(fi.iterators) == 0 {
		return false
	}
	if !fi.initiated {
		// Don't forward first time -- we had to 'Next' once in order to
		// do the sorting already
		fi.initiated = true
		return true
	}
	return fi.innerNext(0)
}

// innerNext handles the next operation internally,
// and should be invoked when we know that two elements in the list may have
// the same value.
// For example, if the list becomes [2,3,5,5,8,9,10], then we should invoke
// innerNext(3), which will call Next on elem 3 (the second '5'). It will continue
// along the list and apply the same operation if needed
func (fi *fastIterator) innerNext(pos int) bool {
	if !fi.iterators[pos].Next() {
		//Exhausted, remove this iterator
		fi.remove(pos)
		if len(fi.iterators) == 0 {
			return false
		}
		return true
	}
	if pos == len(fi.iterators)-1 {
		// Only one iterator left
		return true
	}
	// We next:ed the elem at 'pos'. Now we may have to re-sort that elem
	val, neighbour := fi.iterators[pos].Key(), fi.iterators[pos+1].Key()
	diff := bytes.Compare(val[:], neighbour[:])
	if diff < 0 {
		// It is still in correct place
		return true
	}
	if diff == 0 {
		// It has same value as the neighbour. So still in correct place, but
		// we need to iterate on the neighbour
		fi.innerNext(pos + 1)
		return true
	}
	// At this point, the elem is in the wrong location, but the
	// remaining list is sorted. Find out where to move the elem
	iterationNeeded := false
	index := sort.Search(len(fi.iterators), func(n int) bool {
		if n <= pos {
			// No need to search 'behind' us
			return false
		}
		if n == len(fi.iterators)-1 {
			// Can always place an elem last
			return true
		}
		neighbour := fi.iterators[n+1].Key()
		diff := bytes.Compare(val[:], neighbour[:])
		if diff == 0 {
			// The elem we're placing it next to has the same value,
			// so it's going to need further iteration
			iterationNeeded = true
		}
		return diff < 0
	})
	fi.move(pos, index)
	if iterationNeeded {
		fi.innerNext(index)
	}
	return true
}

// move moves an iterator to another position in the list
func (fi *fastIterator) move(index, newpos int) {
	if newpos > len(fi.iterators)-1 {
		newpos = len(fi.iterators) - 1
	}
	var (
		elem   = fi.iterators[index]
		middle = fi.iterators[index+1 : newpos+1]
		suffix []Iterator
	)
	if newpos < len(fi.iterators)-1 {
		suffix = fi.iterators[newpos+1:]
	}
	fi.iterators = append(fi.iterators[:index], middle...)
	fi.iterators = append(fi.iterators, elem)
	fi.iterators = append(fi.iterators, suffix...)
}

// remove drops an iterator from the list
func (fi *fastIterator) remove(index int) {
	fi.iterators = append(fi.iterators[:index], fi.iterators[index+1:]...)
}

// Key returns the current key
func (fi *fastIterator) Key() common.Hash {
	return fi.iterators[0].Key()
}

// The fast iterator does not query parents as much.
func (dl *diffLayer) newFastIterator() Iterator {
	var seen = make(map[common.Hash]struct{})
	all_iterators := dl.iterators()
	var iterators []Iterator
	// An initial sorting of the iterators is needed. We can't use
	// sort.Sort immediately, until we have deduped the iterators keys
	for _, it := range all_iterators {
		for {
			if !it.Next() {
				break
			}
			v := it.Key()
			if _, exist := seen[v]; !exist {
				seen[v] = struct{}{}
				iterators = append(iterators, it)
				break
			}
		}
	}
	f := &fastIterator{iterators, false}
	sort.Sort(f)
	return f
}

// Debug is a convencience helper during testing
func (fi *fastIterator) Debug() {
	for _, it := range fi.iterators {
		fmt.Printf(" %v ", it.Key()[31])
	}
	fmt.Println()
}
