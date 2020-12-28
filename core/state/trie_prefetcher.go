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
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"sync"
)

var (
	// trieDeliveryMeter counts how many times the prefetcher was unable to supply
	// the statedb with a prefilled trie. This meter should be zero -- if it's not, that
	// needs to be investigated
	trieDeliveryMissMeter = metrics.NewRegisteredMeter("trie/prefetch/deliverymiss", nil)

	triePrefetchFetchMeter = metrics.NewRegisteredMeter("trie/prefetch/fetch", nil)
	triePrefetchSkipMeter  = metrics.NewRegisteredMeter("trie/prefetch/skip", nil)
	triePrefetchDropMeter  = metrics.NewRegisteredMeter("trie/prefetch/drop", nil)
)

// TriePrefetcher is an active prefetcher, which receives accounts or storage
// items on two channels, and does trie-loading of the items.
// The goal is to get as much useful content into the caches as possible
type TriePrefetcher struct {
	storageReqCh chan *storageFetchRequest
	accountReqCh chan *accountFetchRequest

	accountCtrlCh chan (common.Hash) // Chan to control activity, pause/new root
	storageCtrlCh chan (common.Hash) // Chan to control activity, pause/new root
	quitCh        chan (struct{})    // Chan to signal when it's time to exit
	deliveryCh    chan (struct{})
	db            Database

	paused bool

	storageTries    map[common.Hash]Trie
	accountTrie     Trie
	accountTrieRoot common.Hash

	wg sync.WaitGroup
}

func NewTriePrefetcher(db Database) *TriePrefetcher {
	return &TriePrefetcher{
		storageReqCh:  make(chan *storageFetchRequest, 100),
		accountReqCh:  make(chan *accountFetchRequest, 100),
		accountCtrlCh: make(chan common.Hash),
		storageCtrlCh: make(chan common.Hash),
		quitCh:        make(chan struct{}),
		deliveryCh:    make(chan struct{}),
		db:            db,
	}
}

type accountFetchRequest struct {
	addresses []common.Address
}

type storageFetchRequest struct {
	slots       []common.Hash
	storageRoot common.Hash
}

func (p *TriePrefetcher) loopAccounts() {
	var (
		accountTrieRoot common.Hash
		accountTrie     Trie
		skipped         int64
		fetched         int64
		paused          = true
	)
	defer p.wg.Done()
	var drain = func() {
		for {
			select {
			case req := <-p.accountReqCh:
				skipped += int64(len(req.addresses))
			default:
				break
			}
		}
	}

	for {
		select {
		case <-p.quitCh:
			return
		case stateRoot := <-p.accountCtrlCh:
			// Clear out any (now stale) requests
			drain()
			// Sanity checks for programming errors
			if paused && stateRoot == (common.Hash{}) {
				log.Error("Trie prefetcher unpaused with bad root")
			} else if !paused && stateRoot != (common.Hash{}) {
				log.Error("Trie prefetcher paused with non-empty root")
			}
			if paused { // paused -> active
				// Clear old data
				p.accountTrie = nil
				p.accountTrieRoot = common.Hash{}
				// Resume again
				accountTrieRoot = stateRoot
				var err error
				if accountTrie, err = p.db.OpenTrie(accountTrieRoot); err != nil {
					log.Error("Trie prefetcher failed opening trie", "root", accountTrieRoot, "err", err)
				}
				paused = false
			} else { // active -> paused
				// Update metrics at new block events
				triePrefetchFetchMeter.Mark(fetched)
				triePrefetchSkipMeter.Mark(skipped)
				fetched, skipped = 0, 0
				// Make the prefetched tries accessible to the external world
				p.accountTrie = accountTrie
				p.accountTrieRoot = accountTrieRoot
				paused = true
			}
			// Signal that we're done, external callers can access the
			// account-fields that are preloaded
			p.deliveryCh <- struct{}{}
		case req := <-p.accountReqCh:
			if paused {
				continue
			}
			for _, addr := range req.addresses {
				accountTrie.TryGet(addr[:])
			}
			fetched += int64(len(req.addresses))
		}
	}
}

func (p *TriePrefetcher) loopStorage() {
	var (
		storageTries map[common.Hash]Trie
		skipped      int64
		fetched      int64
		paused       = true
	)
	defer p.wg.Done()
	var drain = func() {
		for {
			select {
			case req := <-p.storageReqCh:
				skipped += int64(len(req.slots))
			default:
				break
			}
		}
	}
	for {
		select {
		case <-p.quitCh:
			return
		case stateRoot := <-p.storageCtrlCh:
			// Clear out any (now stale) requests
			drain()
			// Sanity checks for programming errors
			if paused && stateRoot == (common.Hash{}) {
				log.Error("Trie prefetcher unpaused with bad root")
			} else if !paused && stateRoot != (common.Hash{}) {
				log.Error("Trie prefetcher paused with non-empty root")
			}
			if paused {
				p.storageTries = nil // Clear old data
				storageTries = make(map[common.Hash]Trie)
				paused = false // Resume again
			} else {
				// Update metrics at new block events
				triePrefetchFetchMeter.Mark(fetched)
				triePrefetchSkipMeter.Mark(skipped)
				fetched, skipped = 0, 0
				// Make the prefetched tries accessible to the external world
				p.storageTries = storageTries
				paused = true
			}
			p.deliveryCh <- struct{}{}
		case req := <-p.storageReqCh:
			if paused {
				continue
			}
			sRoot := req.storageRoot
			// Storage slots to fetch
			var (
				storageTrie Trie
				err         error
			)
			if storageTrie = storageTries[sRoot]; storageTrie == nil {
				if storageTrie, err = p.db.OpenTrie(sRoot); err != nil {
					log.Warn("trie prefetcher failed opening storage trie", "root", sRoot, "err", err)
					skipped += int64(len(req.slots))
					continue
				}
				storageTries[sRoot] = storageTrie
			}
			for _, key := range req.slots {
				storageTrie.TryGet(key[:])
			}
			fetched += int64(len(req.slots))
		}
	}
}

// Start starts the two prefetcher loops
func (p *TriePrefetcher) Start() {
	p.wg.Add(2)
	go p.loopAccounts()
	go p.loopStorage()
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
// fetch data concerning the new root
func (p *TriePrefetcher) Resume(root common.Hash) {
	p.paused = false
	// Send to both loops
	p.accountCtrlCh <- root
	p.storageCtrlCh <- root
	// Wait for both loops to signal readyness
	<-p.deliveryCh
	<-p.deliveryCh
}

// Pause causes the prefetcher to pause prefetching, and make tries
// accessible to callers via GetTrie
func (p *TriePrefetcher) Pause() {
	if p.paused {
		return
	}
	p.paused = true
	// Send to both loops
	p.accountCtrlCh <- common.Hash{}
	p.storageCtrlCh <- common.Hash{}
	// Wait for both loops to signal paused
	<-p.deliveryCh
	<-p.deliveryCh
}

// PrefetchAddresses adds an address for prefetching
func (p *TriePrefetcher) PrefetchAddresses(addresses []common.Address) {
	// We do an async send here, to not cause the caller to block
	select {
	case p.accountReqCh <- &accountFetchRequest{
		addresses: addresses,
	}:
	default:
		triePrefetchDropMeter.Mark(int64(len(addresses)))
	}
}

// PrefetchStorage adds a storage root and a set of keys for prefetching
func (p *TriePrefetcher) PrefetchStorage(root common.Hash, slots []common.Hash) {
	// We do an async send here, to not cause the caller to block
	select {
	case p.storageReqCh <- &storageFetchRequest{
		storageRoot: root,
		slots:       slots,
	}:
	default:
		triePrefetchDropMeter.Mark(int64(len(slots)))
	}
}

// GetTrie returns the trie matching the root hash, or nil if the prefetcher
// doesn't have it.
func (p *TriePrefetcher) GetTrie(root common.Hash) Trie {
	if root == p.accountTrieRoot {
		return p.accountTrie
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
