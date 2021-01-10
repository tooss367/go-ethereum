// Copyright 2020 The go-ethereum Authors
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

package state

import (
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

var (
	// trieDeliveryMeter counts how many times the prefetcher was unable to supply
	// the statedb with a prefilled trie. This meter should be zero -- if it's not, that
	// needs to be investigated
	trieDeliveryMissMeter = metrics.NewRegisteredMeter("trie/prefetch/deliverymiss", nil)

	triePrefetchAccountLoadMeter  = metrics.NewRegisteredMeter("trie/prefetch/account/load", nil)
	triePrefetchAccountDupMeter   = metrics.NewRegisteredMeter("trie/prefetch/account/dup", nil)
	triePrefetchAccountSkipMeter  = metrics.NewRegisteredMeter("trie/prefetch/account/skip", nil)
	triePrefetchAccountWasteMeter = metrics.NewRegisteredMeter("trie/prefetch/account/waste", nil)

	triePrefetchStorageLoadMeter  = metrics.NewRegisteredMeter("trie/prefetch/storage/load", nil)
	triePrefetchStorageDupMeter   = metrics.NewRegisteredMeter("trie/prefetch/storage/dup", nil)
	triePrefetchStorageSkipMeter  = metrics.NewRegisteredMeter("trie/prefetch/storage/skip", nil)
	triePrefetchStorageWasteMeter = metrics.NewRegisteredMeter("trie/prefetch/storage/waste", nil)
)

// TriePrefetcher is an active prefetcher, which receives accounts or storage
// items and does trie-loading of them. The goal is to get as much useful content
// into the caches as possible.
//
// Note, the prefetcher's API is not thread safe.
type TriePrefetcher struct {
	db Database

	accountCmdCh    chan *command // Control channel to pause or reset the root
	accountReqCh    chan struct{} // Notification channel for new preload requests
	accountTaskLock sync.Mutex    // Lock protecting the task sets
	accountPaused   uint32        // Whether the prefetcher is actively loading data or serving tries

	storageCmdCh    chan *command // Control channel to pause or reset the root
	storageReqCh    chan struct{} // Notification channel for new preload requests
	storageTaskLock sync.Mutex    // Lock protecting the task sets
	storagePaused   uint32        // Whether the prefetcher is actively loading data or serving tries

	quitCh chan struct{}

	accountTasks     []common.Address                         // Set of accounts to prefetch
	storageTasks     map[common.Hash][]common.Hash            // Set of storage slots to prefetch
	accountTasksDone map[common.Address]struct{}              // Set of accounts prefetched
	storageTasksDone map[common.Hash]map[common.Hash]struct{} // Set of storage slots prefetched

	storageTries    map[common.Hash]Trie // Prefetched storage tries
	accountTrie     Trie                 // Prefetched account trie
	accountTrieRoot common.Hash          // Root of the prefetched account trie

	wg sync.WaitGroup
}

func NewTriePrefetcher(db Database) *TriePrefetcher {
	return &TriePrefetcher{
		db: db,

		accountCmdCh:  make(chan *command),
		accountReqCh:  make(chan struct{}, 1), // 1 to notify, no need to track multiple notifications
		accountPaused: 1,                      // User needs to call Resume() before allowing to preload data

		storageCmdCh:  make(chan *command),
		storageReqCh:  make(chan struct{}, 1), // 1 to notify, no need to track multiple notifications
		storagePaused: 1,                      // User needs to call Resume() before allowing to preload data

		quitCh:       make(chan struct{}),
		storageTasks: make(map[common.Hash][]common.Hash),
	}
}

// Start starts the two prefetcher loops
func (p *TriePrefetcher) Start() {
	p.wg.Add(2)
	go p.loopAccounts()
	go p.loopStorage()
}

type command struct {
	root common.Hash
	done chan struct{}
}

func (p *TriePrefetcher) loopAccounts() {
	defer p.wg.Done()
	var (
		accountTrieRoot common.Hash
		accountTrie     Trie

		err error

		// Some tracking of performance
		accountLoads, accountDups, accountSkips, accountWastes int64

		paused = true
	)
	// The prefetcher loop has two distinct phases:
	// 1: Paused: when in this state, the accumulated tries are accessible to outside
	// callers.
	// 2: Active prefetching, awaiting slots and accounts to prefetch
	for {
		select {
		case <-p.quitCh:
			return
		case cmd := <-p.accountCmdCh:
			// Clear out any pending update notification
			select {
			case <-p.accountReqCh: // Skip stale notification
			default: // No notification queued
			}

			if paused {
				// Prefetcher is being resumed, mark any old data as waste
				p.accountTaskLock.Lock()
				accountWastes += int64(len(p.accountTasksDone))
				p.accountTasksDone = make(map[common.Address]struct{})
				p.accountTaskLock.Unlock()

				triePrefetchAccountWasteMeter.Mark(accountWastes)
				accountWastes = 0

				// Start with a new set of tries
				p.accountTrie = nil
				p.accountTrieRoot = common.Hash{}

				accountTrieRoot = cmd.root
				accountTrie, err = p.db.OpenTrie(accountTrieRoot)
				if err != nil {
					log.Error("Trie prefetcher failed opening trie", "root", accountTrieRoot, "err", err)
				}
				if accountTrieRoot == (common.Hash{}) {
					log.Error("Trie prefetcher unpaused with bad root")
				}
				paused = false
			} else {
				// Prefetcher is being paused, update all metrics
				p.accountTaskLock.Lock()
				accountSkips += int64(len(p.accountTasks))
				p.accountTasks = nil
				p.accountTaskLock.Unlock()

				triePrefetchAccountLoadMeter.Mark(accountLoads)
				triePrefetchAccountDupMeter.Mark(accountDups)
				triePrefetchAccountSkipMeter.Mark(accountSkips)

				accountLoads, accountDups, accountSkips = 0, 0, 0

				// Make the tries accessible
				p.accountTrie = accountTrie
				p.accountTrieRoot = accountTrieRoot

				if cmd.root != (common.Hash{}) {
					log.Error("Trie prefetcher paused with non-empty root")
				}
				paused = true
			}
			cmd.done <- struct{}{}

		case <-p.accountReqCh:
			if paused {
				log.Error("Prefetch request arrived whilst paused")
				continue
			}
			// Retrieve all the tasks queued up and reset the sets for new insertions
			p.accountTaskLock.Lock()
			accountTasks := p.accountTasks
			p.accountTasks = nil
			p.accountTaskLock.Unlock()

			// Keep prefetching the data until an interruption is triggered
			for _, addr := range accountTasks {
				if atomic.LoadUint32(&p.accountPaused) == 1 {
					break
				}
				if _, ok := p.accountTasksDone[addr]; ok {
					accountDups++
				} else {
					accountTrie.TryGet(addr[:])
					p.accountTasksDone[addr] = struct{}{}
					accountLoads++
				}
				accountTasks = accountTasks[1:]
			}

			// If pre-fetching was interrupted, return all remaining tasks into the queue
			if atomic.LoadUint32(&p.accountPaused) == 1 {
				p.accountTaskLock.Lock()
				p.accountTasks = append(p.accountTasks, accountTasks...)
				p.accountTaskLock.Unlock()
			}
		}
	}
}

func (p *TriePrefetcher) loopStorage() {
	defer p.wg.Done()
	var (
		storageTries map[common.Hash]Trie
		// Some tracking of performance
		storageLoads, storageDups, storageSkips, storageWastes int64
		paused                                                 = true
	)
	// The prefetcher loop has two distinct phases:
	// 1: Paused: when in this state, the accumulated tries are accessible to outside
	// callers.
	// 2: Active prefetching, awaiting slots and accounts to prefetch
	for {
		select {
		case <-p.quitCh:
			return
		case cmd := <-p.storageCmdCh:
			// Clear out any pending update notification
			select {
			case <-p.storageReqCh: // Skip stale notification
			default: // No notification queued
			}

			if paused {
				// Prefetcher is being resumed, mark any old data as waste
				p.storageTaskLock.Lock()
				for storageTasksDone := range p.storageTasksDone {
					storageWastes += int64(len(storageTasksDone))
				}
				p.storageTasksDone = make(map[common.Hash]map[common.Hash]struct{})
				p.storageTaskLock.Unlock()

				triePrefetchStorageWasteMeter.Mark(storageWastes)
				storageWastes = 0

				// Start with a new set of tries
				p.storageTries = nil
				storageTries = make(map[common.Hash]Trie)
				paused = false
			} else {
				// Prefetcher is being paused, update all metrics
				p.storageTaskLock.Lock()
				for storageTasks := range p.storageTasks {
					storageSkips += int64(len(storageTasks))
				}
				p.storageTasks = make(map[common.Hash][]common.Hash)
				p.storageTaskLock.Unlock()

				triePrefetchStorageLoadMeter.Mark(storageLoads)
				triePrefetchStorageDupMeter.Mark(storageDups)
				triePrefetchStorageSkipMeter.Mark(storageSkips)

				storageLoads, storageDups, storageSkips = 0, 0, 0

				// Make the tries accessible
				p.storageTries = storageTries
				paused = true
			}
			cmd.done <- struct{}{}

		case <-p.storageReqCh:
			if paused {
				log.Error("Prefetch request arrived whilst paused")
				continue
			}
			// Retrieve all the tasks queued up and reset the sets for new insertions
			p.storageTaskLock.Lock()
			storageTasks := p.storageTasks
			p.storageTasks = make(map[common.Hash][]common.Hash)
			p.storageTaskLock.Unlock()

			// Prefetch storage tries until interrupt
			for root, slots := range storageTasks {
				if atomic.LoadUint32(&p.storagePaused) == 1 {
					break
				}
				if _, ok := storageTries[root]; !ok {
					storageTrie, err := p.db.OpenTrie(root)
					if err != nil {
						log.Warn("Trie prefetcher failed opening storage trie", "root", root, "err", err)
						storageSkips += int64(len(slots))
						continue
					}
					if _, ok := p.storageTasksDone[root]; !ok {
						p.storageTasksDone[root] = make(map[common.Hash]struct{})
					}
					storageTries[root] = storageTrie
				}
				slotTasksDone := p.storageTasksDone[root]
				storageTrie := storageTries[root]
				for _, hash := range slots {
					if atomic.LoadUint32(&p.storagePaused) == 1 {
						break
					}
					if _, ok := slotTasksDone[hash]; ok {
						storageDups++
					} else {
						storageTrie.TryGet(hash[:])
						slotTasksDone[hash] = struct{}{}
						storageLoads++
					}
					slots = slots[1:]
				}
				if len(slots) == 0 {
					delete(storageTasks, root)
				} else {
					storageTasks[root] = slots
				}
			}
			// If pre-fetching was interrupted, return all remaining tasks into the queue
			if atomic.LoadUint32(&p.storagePaused) == 1 {
				p.storageTaskLock.Lock()
				for root, slots := range storageTasks {
					p.storageTasks[root] = append(p.storageTasks[root], slots...)
				}
				p.storageTaskLock.Unlock()
			}
		}
	}
}

// Close stops the prefetcher
func (p *TriePrefetcher) Close() {
	if p.quitCh != nil {
		close(p.quitCh)
		p.quitCh = nil
		p.wg.Wait()
	}
}

// Resume causes the prefetcher to clear out old data, and get ready to
// fetch data concerning the new root.
func (p *TriePrefetcher) Resume(root common.Hash) {
	// Abort if the prefetcher is not paused
	if atomic.LoadUint32(&p.accountPaused) == 0 {
		log.Error("Trie prefetcher already resumed")
		return
	}
	atomic.StoreUint32(&p.accountPaused, 0)
	atomic.StoreUint32(&p.storagePaused, 0)

	cmd := &command{
		root: root,
		done: make(chan struct{}, 1),
	}
	p.accountCmdCh <- cmd
	p.storageCmdCh <- cmd
	<-cmd.done
	<-cmd.done
}

// Pause causes the prefetcher to pause prefetching, and make tries
// accessible to callers via GetTrie.
func (p *TriePrefetcher) PauseAccounts() {
	// Abort if the prefetcher is already paused
	if atomic.LoadUint32(&p.accountPaused) == 1 {
		log.Error("Account trie prefetcher already paused")
		return
	}
	atomic.StoreUint32(&p.accountPaused, 1)

	// Request a pause and wait until it's done
	cmd := &command{
		done: make(chan struct{}),
	}
	p.accountCmdCh <- cmd
	<-cmd.done
}

func (p *TriePrefetcher) PauseStorage() {
	// Abort if the prefetcher is already paused
	if atomic.LoadUint32(&p.storagePaused) == 1 {
		log.Error("Storage trie prefetcher already paused")
		return
	}
	atomic.StoreUint32(&p.storagePaused, 1)

	// Request a pause and wait until it's done
	cmd := &command{
		done: make(chan struct{}),
	}
	p.storageCmdCh <- cmd
	<-cmd.done
}

// PrefetchAddresses adds an address for prefetching
func (p *TriePrefetcher) PrefetchAddresses(addrs []common.Address) {
	// Abort if the prefetcher is already paused
	if atomic.LoadUint32(&p.accountPaused) == 1 {
		log.Error("Attempted account trie-prefetch whilst paused")
		return
	}
	// Inject the addresses into the task queue and notify the prefetcher
	p.accountTaskLock.Lock()
	defer p.accountTaskLock.Unlock()

	p.accountTasks = append(p.accountTasks, addrs...)
	select {
	case p.accountReqCh <- struct{}{}:
	default:
		// Already notified
	}
}

// PrefetchStorage adds a storage root and a set of keys for prefetching
func (p *TriePrefetcher) PrefetchStorage(root common.Hash, slots []common.Hash) {
	// Abort if the prefetcher is already paused
	if atomic.LoadUint32(&p.storagePaused) == 1 {
		log.Error("Attempted storage trie-prefetch whilst paused")
		return
	}
	// Inject the storage hashes into the task queue and notify the prefetcher
	p.storageTaskLock.Lock()
	defer p.storageTaskLock.Unlock()

	p.storageTasks[root] = append(p.storageTasks[root], slots...)
	select {
	case p.storageReqCh <- struct{}{}:
	default:
		// Already notified
	}
}

// GetAccountTrie returns the trie matching the root hash, or nil if the prefetcher
// doesn't have it.
func (p *TriePrefetcher) GetAccountTrie(root common.Hash) Trie {
	// Abort if the prefetcher is not paused
	if atomic.LoadUint32(&p.accountPaused) == 0 {
		log.Error("Attempted trie-prefetcher retrieval whilst not paused")
		return nil
	}
	if root == p.accountTrieRoot {
		return p.accountTrie
	}
	return nil
}

// GetStorageTrie returns the trie matching the root hash, or nil if the prefetcher
// doesn't have it.
func (p *TriePrefetcher) GetStorageTrie(root common.Hash) Trie {
	if atomic.LoadUint32(&p.storagePaused) == 0 {
		log.Error("Attempted storage trie-prefetcher retrieval whilst not paused")
		return nil
	}
	if storageTrie, ok := p.storageTries[root]; ok {
		// Two accounts may well have the same storage root, but we cannot allow
		// them both to make updates to the same trie instance. Therefore,
		// we need to either delete the trie now, or deliver a copy of the trie.
		delete(p.storageTries, root)
		return storageTrie
	}
	trieDeliveryMissMeter.Mark(1)
	return nil
}

// UseAddresses marks a batch of addresses used to allow creating statistics as to
// how useful or wasteful the prefetcher is.
// TODO: Remove this later on, useful for debugging/testing
func (p *TriePrefetcher) UseAddresses(addrs []common.Address) {
	// Abort if the prefetcher is not paused
	if atomic.LoadUint32(&p.accountPaused) == 0 {
		log.Error("Attempted account usage marking whilst not paused")
		return
	}
	// Drop the used addresses from the finished task sets
	p.accountTaskLock.Lock()
	defer p.accountTaskLock.Unlock()

	for _, addr := range addrs {
		delete(p.accountTasksDone, addr)
	}
}

// UseStorage marks a batch of storage slots used to allow creating statistics
// as to how useful or wasteful the prefetcher is.
// TODO: Remove this later on, useful for debugging/testing
func (p *TriePrefetcher) UseStorage(root common.Hash, slots []common.Hash) {
	// Abort if the prefetcher is not paused
	if atomic.LoadUint32(&p.storagePaused) == 0 {
		log.Error("Attempted account usage marking whilst not paused")
		return
	}
	// Drop the used storage slots from the finished task sets
	p.storageTaskLock.Lock()
	defer p.storageTaskLock.Unlock()

	slotTasksDone := p.storageTasksDone[root]
	for _, hash := range slots {
		delete(slotTasksDone, hash)
	}
	if len(slotTasksDone) == 0 {
		delete(p.storageTasksDone, root)
	}
}
