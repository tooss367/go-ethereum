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
	"bufio"
	"fmt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

// verifierStats is a collection of statistics gathered by the verifier
// for logging purposes.
type verifierStats struct {
	start       time.Time // Timestamp when generation started
	lastLog     time.Time
	nodes       uint64 // number of nodes loaded
	accounts    uint64 // Number of accounts loaded
	slots       uint64 // Number of storage slots checked
	lastAccount []byte
	path        []byte
}

func (vs *verifierStats) Log(msg string) {
	ctx := []interface{}{
		"elapsed", time.Since(vs.start),
		"nodes", vs.nodes, "accounts", vs.accounts, "slots", vs.slots,
		"lastAccount", fmt.Sprintf("0x%x", vs.lastAccount),
		"path", fmt.Sprintf("0x%x", vs.path),
	}
	log.Info(msg, ctx...)
	vs.lastLog = time.Now()
}

// verifyStorageTrie checks a given trie. If the trie is missing, or the trie
// contains errors, the hashes of all nodes leading to the missing item
// are returned.
// This method may return zero hashes, which means that the storage trie
// itself is missing
func verifyStorageTrie(root common.Hash, db Database, vs *verifierStats) (error, []common.Hash) {

	storageTrie, err := db.OpenStorageTrie(common.Hash{}, root)

	if err != nil {
		// The trie storage root is missing. TODO handle this error
		return fmt.Errorf("Missing storage root: %w", err), nil
	}
	it := storageTrie.NodeIterator(nil)

	for it.Next(true) {
		vs.path = it.Path()
		vs.nodes++
		if time.Since(vs.lastLog) > 8*time.Second {
			vs.Log("Verifying storage trie")
		}
		if it.Leaf() {
			vs.slots++
		}
	}
	if err = it.Error(); err != nil {
		// We have hit an error. Now figure out the parents
		var parents []common.Hash
		path := it.Path()
		log.Error("Storage trie error", "path", fmt.Sprintf("%x", path),
			"hash", it.Hash(), "parent", it.Parent(), "error", err)

		if it.Parent() != (common.Hash{}) {
			fmt.Println("Elements that need to be removed:\n")
		}
		for {
			if ok, _ := trie.Pop(it); !ok {
				break
			}
			fmt.Printf("%x\n", it.Hash())
			parents = append(parents, it.Hash())
			log.Error("Parent ", "hash", it.Hash(), "parent", it.Parent(),
				"path", fmt.Sprintf("0x%x", it.Path()))
		}
		return fmt.Errorf("storage trie error: %w", err), parents
	}

	return nil, nil
}

// Repair returns 'true' if anything was changed
func (s *StateDB) Repair(diskdb ethdb.Database) bool {
	err, hashes := s.Verify(nil)
	if err == nil {
		return false
	}
	msg := fmt.Sprintf(`
The state verification found at least one missing node. In order to perform 
a "healing fast-sync", %d parent nodes needs to be removed from the database. 

Once this is done, you can start geth normally (mode=fast), and geth should finish repairing 
the state trie. 

If you were running an archive node, this operation will most definitely lead to some states
being inaccessible, since the repair will be based off the tip of the chain, not the point at
which your node is currently at.

Do you wish to proceed? [y/N]
`, len(hashes))
	reader := bufio.NewReader(os.Stdin)
	fmt.Println(msg)
	text, _ := reader.ReadString('\n')
	if text != "y" && text != "Y" {
		return false
	}
	for _, h := range hashes {
		// Delete the hash from the database
		fmt.Printf("Deleting hash %x\n", h)
		diskdb.Delete(h[:])
	}
	// Now, we have to fool geth to think it's in the middle of an
	// interrupted fast-sync
	genesis := rawdb.ReadCanonicalHash(diskdb, 0)
	log.Info("Writing genesis as headblock", "hash", genesis)

	// Put the genesis in there
	rawdb.WriteHeadBlockHash(diskdb, genesis)
	return true
}

func (s *StateDB) Verify(start []byte) (error, []common.Hash) {
	log.Info("Starting verification procedure")
	var (
		vs = &verifierStats{
			start:    time.Now(),
			nodes:    0,
			accounts: 0,
			slots:    0,
			path:     []byte{},
		}
		it      = s.trie.NodeIterator(start)
		nodes   = uint64(0)
		err     error
		parents []common.Hash
		// Avoid rechecking storage
		checkedStorageTries = make(map[common.Hash]struct{})
	)
	for it.Next(true) {
		vs.path = it.Path()
		vs.nodes++
		vs.nodes++
		if it.Leaf() {
			vs.accounts++
			vs.lastAccount = it.LeafKey()
			// We might have to iterate a storage trie
			//accountHash := common.BytesToHash(it.LeafKey())
			var acc struct {
				Nonce    uint64
				Balance  []byte // big.int can be decoded as byte slice
				Root     common.Hash
				CodeHash []byte
			}
			if err := rlp.DecodeBytes(it.LeafBlob(), &acc); err != nil {
				log.Crit("Invalid account encountered during verification", "err", err)
			}
			if _, checked := checkedStorageTries[acc.Root]; checked {
				continue
			}
			checkedStorageTries[acc.Root] = struct{}{}
			if err, parents = verifyStorageTrie(acc.Root, s.db, vs); err != nil {
				// This account is bad.
				break
			}
		}
		if time.Since(vs.lastLog) > 8*time.Second {
			vs.Log("Verifying state trie")
		}
	}
	if err == nil {
		err = it.Error()
	}
	if err != nil {
		// We have hit an error. Now figure out the parents
		path := it.Path()
		log.Error("Trie error", "path", fmt.Sprintf("%x", path),
			"hash", it.Hash(), "parent", it.Parent(), "error", err)

		for {
			if ok, _ := trie.Pop(it); !ok {
				break
			}
			fmt.Printf("%x\n", it.Hash())
			log.Error("Parent ", "hash", it.Hash(), "parent", it.Parent(),
				"path", fmt.Sprintf("0x%x", it.Path()))
			parents = append(parents, it.Hash())
		}
		if len(parents) > 0 {
			fmt.Println("Elements that need to be removed:\n")
			for _, h := range parents {
				fmt.Printf("%x\n", h)
			}
		}
		return fmt.Errorf("trie error: %w", err), parents
	}

	log.Info("Verified state trie", "elapsed", time.Since(vs.start), "nodes", nodes)
	return nil, nil
}
