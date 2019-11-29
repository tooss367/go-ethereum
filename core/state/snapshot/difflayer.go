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
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/steakknife/bloomfilter"
)

// diffLayer represents a collection of modifications made to a state snapshot
// after running a block on top. It contains one sorted list for the account trie
// and one-one list for each storage tries.
//
// The goal of a diff layer is to act as a journal, tracking recent modifications
// made to the state, that have not yet graduated into a semi-immutable state.
type diffLayer struct {
	origin *diskLayer // Base disk layer to directly use on bloom misses
	parent snapshot   // Parent snapshot modified by this one, never nil
	memory uint64     // Approximate guess as to how much memory we use

	root  common.Hash // Root hash to which this snapshot diff belongs to
	stale bool        // Signals that the layer became stale (state progressed)

	accountList []common.Hash                          // List of account for iteration. If it exists, it's sorted, otherwise it's nil
	accountData map[common.Hash][]byte                 // Keyed accounts for direct retrival (nil means deleted)
	storageList map[common.Hash][]common.Hash          // List of storage slots for iterated retrievals, one per account. Any existing lists are sorted if non-nil
	storageData map[common.Hash]map[common.Hash][]byte // Keyed storage slots for direct retrival. one per account (nil means deleted)

	diffed *bloomfilter.Filter // Bloom filter tracking all the diffed items up to the disk layer

	lock sync.RWMutex
}

// accountBloomHasher is a wrapper around a common.Hash to satisfy the interface
// API requirements of the bloom library used. It's used to convert an account
// hash into a 64 bit mini hash.
type accountBloomHasher common.Hash

func (h accountBloomHasher) Write(p []byte) (n int, err error) { panic("not implemented") }
func (h accountBloomHasher) Sum(b []byte) []byte               { panic("not implemented") }
func (h accountBloomHasher) Reset()                            { panic("not implemented") }
func (h accountBloomHasher) BlockSize() int                    { panic("not implemented") }
func (h accountBloomHasher) Size() int                         { return 8 }
func (h accountBloomHasher) Sum64() uint64 {
	return binary.BigEndian.Uint64(h[:8])
}

// storageBloomHasher is a wrapper around a [2]common.Hash to satisfy the interface
// API requirements of the bloom library used. It's used to convert an account
// hash into a 64 bit mini hash.
type storageBloomHasher [2]common.Hash

func (h storageBloomHasher) Write(p []byte) (n int, err error) { panic("not implemented") }
func (h storageBloomHasher) Sum(b []byte) []byte               { panic("not implemented") }
func (h storageBloomHasher) Reset()                            { panic("not implemented") }
func (h storageBloomHasher) BlockSize() int                    { panic("not implemented") }
func (h storageBloomHasher) Size() int                         { return 8 }
func (h storageBloomHasher) Sum64() uint64 {
	return binary.BigEndian.Uint64(h[0][:8]) ^ binary.BigEndian.Uint64(h[1][:8])
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
	switch parent := parent.(type) {
	case *diskLayer:
		dl.rebloom(parent)
	case *diffLayer:
		dl.rebloom(parent.origin)
	default:
		panic("unknown parent type")
	}
	// Determine memory size and track the dirty writes
	for _, data := range accounts {
		dl.memory += uint64(common.HashLength + len(data))
		snapshotDirtyAccountWriteMeter.Mark(int64(len(data)))
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
		// Determine memory size and track the dirty writes
		for _, data := range slots {
			dl.memory += uint64(common.HashLength + len(data))
			snapshotDirtyStorageWriteMeter.Mark(int64(len(data)))
		}
	}
	dl.memory += uint64(len(dl.storageList) * common.HashLength)
	return dl
}

// rebloom discards the layer's current bloom and rebuilds it from scratch based
// on the parent's and the local diffs.
func (dl *diffLayer) rebloom(origin *diskLayer) {
	dl.lock.Lock()
	defer dl.lock.Unlock()

	defer func(start time.Time) {
		snapshotBloomIndexTimer.Update(time.Since(start))
	}(time.Now())

	// Inject the new origin that triggered the rebloom
	dl.origin = origin

	// Retrieve the parent bloom or create a fresh empty one
	if parent, ok := dl.parent.(*diffLayer); ok {
		parent.lock.RLock()
		dl.diffed, _ = parent.diffed.Copy()
		parent.lock.RUnlock()
	} else {
		dl.diffed, _ = bloomfilter.New(100*1024*8, 3)
	}
	// Iterate over all the accounts and storage slots and index them
	for hash := range dl.accountData {
		dl.diffed.Add(accountBloomHasher(hash))
	}
	for accountHash, slots := range dl.storageData {
		for storageHash := range slots {
			dl.diffed.Add(storageBloomHasher{accountHash, storageHash})
		}
	}
	// Calculate the current false positive rate and update the error rate meter.
	// This is a bit cheating because subsequent layers will overwrite it, but it
	// should be fine, we're only interested in ballpark figures.
	k := float64(dl.diffed.K())
	n := float64(dl.diffed.N())
	m := float64(dl.diffed.M())

	snapshotBloomErrorGauge.Update(math.Pow(1.0-math.Exp((-k)*(n+0.5)/(m-1)), k))
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
	// Check the bloom filter first whether there's even a point in reaching into
	// all the maps in all the layers below
	dl.lock.RLock()
	hit := dl.diffed.Contains(accountBloomHasher(hash))
	dl.lock.RUnlock()

	// If the bloom filter misses, don't even bother with traversing the memory
	// diff layers, reach straight into the bottom persistent disk layer
	if !hit {
		snapshotBloomAccountMissMeter.Mark(1)
		return dl.origin.AccountRLP(hash)
	}
	// The bloom filter hit, start poking in the internal maps
	return dl.accountRLP(hash)
}

// accountRLP is an internal version of AccountRLP that skips the bloom filter
// checks and uses the internal maps to try and retrieve the data. It's meant
// to be used if a higher layer's bloom filter hit already.
func (dl *diffLayer) accountRLP(hash common.Hash) ([]byte, error) {
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
		snapshotDirtyAccountHitMeter.Mark(1)
		snapshotDirtyAccountReadMeter.Mark(int64(len(data)))
		snapshotBloomAccountTrueHitMeter.Mark(1)
		return data, nil
	}
	// Account unknown to this diff, resolve from parent
	if diff, ok := dl.parent.(*diffLayer); ok {
		return diff.accountRLP(hash)
	}
	// Failed to resolve through diff layers, mark a bloom error and use the disk
	snapshotBloomAccountFalseHitMeter.Mark(1)
	return dl.parent.AccountRLP(hash)
}

// Storage directly retrieves the storage data associated with a particular hash,
// within a particular account. If the slot is unknown to this diff, it's parent
// is consulted.
func (dl *diffLayer) Storage(accountHash, storageHash common.Hash) ([]byte, error) {
	// Check the bloom filter first whether there's even a point in reaching into
	// all the maps in all the layers below
	dl.lock.RLock()
	hit := dl.diffed.Contains(storageBloomHasher{accountHash, storageHash})
	dl.lock.RUnlock()

	// If the bloom filter misses, don't even bother with traversing the memory
	// diff layers, reach straight into the bottom persistent disk layer
	if !hit {
		snapshotBloomStorageMissMeter.Mark(1)
		return dl.origin.Storage(accountHash, storageHash)
	}
	// The bloom filter hit, start poking in the internal maps
	return dl.storage(accountHash, storageHash)
}

// storage is an internal version of Storage that skips the bloom filter checks
// and uses the internal maps to try and retrieve the data. It's meant  to be
// used if a higher layer's bloom filter hit already.
func (dl *diffLayer) storage(accountHash, storageHash common.Hash) ([]byte, error) {
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
			snapshotDirtyStorageHitMeter.Mark(1)
			snapshotBloomStorageTrueHitMeter.Mark(1)
			return nil, nil
		}
		if data, ok := storage[storageHash]; ok {
			snapshotDirtyStorageHitMeter.Mark(1)
			snapshotDirtyStorageReadMeter.Mark(int64(len(data)))
			snapshotBloomStorageTrueHitMeter.Mark(1)
			return data, nil
		}
	}
	// Storage slot unknown to this diff, resolve from parent
	if diff, ok := dl.parent.(*diffLayer); ok {
		return diff.storage(accountHash, storageHash)
	}
	// Failed to resolve through diff layers, mark a bloom error and use the disk
	snapshotBloomStorageFalseHitMeter.Mark(1)
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
		diffed:      dl.diffed,
		memory:      parent.memory + dl.memory,
	}
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