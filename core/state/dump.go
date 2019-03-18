// Copyright 2014 The go-ethereum Authors
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
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
)

type DumpAccount struct {
	Balance  string            `json:"balance"`
	Nonce    uint64            `json:"nonce"`
	Root     string            `json:"root"`
	CodeHash string            `json:"codeHash"`
	Code     string            `json:"code"`
	Storage  map[string]string `json:"storage"`
}
type DumpAccountSmall struct {
	Balance string          `json:"balance"`
	Root    string          `json:"root"`
	Code    string          `json:"code"`
	Address common.Address `json:"address"`
}
type Dump struct {
	Root     string                 `json:"root"`
	Accounts map[string]DumpAccount `json:"accounts"`
}

var suffix = []byte{0x6c, 'e', 'x', 'p', 'e', 'r', 'i', 'm', 'e', 'n', 't', 'a', 'l', 0xf5, 0x00, 0x37}

func (self *StateDB) RawDump() Dump {
	dump := Dump{
		Root:     fmt.Sprintf("%x", self.trie.Hash()),
		Accounts: make(map[string]DumpAccount),
	}
	it := trie.NewIterator(self.trie.NodeIterator(nil))
	for it.Next() {
		addr := self.trie.GetKey(it.Key)
		var data Account
		if err := rlp.DecodeBytes(it.Value, &data); err != nil {
			panic(err)
		}
		obj := newObject(nil, common.BytesToAddress(addr), data)
		account := DumpAccount{
			Balance: data.Balance.String(),
			Nonce:   data.Nonce,
			Root:    common.Bytes2Hex(data.Root[:]),
			Code:    common.Bytes2Hex(obj.Code(self.db)),
		}
		dump.Accounts[common.Bytes2Hex(addr)] = account
	}
	return dump
}
func (self *StateDB) LineDump() {
	encoder := json.NewEncoder(os.Stdout)
	it := trie.NewIterator(self.trie.NodeIterator(nil))
	for it.Next() {
		addr := self.trie.GetKey(it.Key)
		var data Account
		if err := rlp.DecodeBytes(it.Value, &data); err != nil {
			panic(err)
		}
		if bytes.Equal(data.CodeHash, emptyCodeHash) {
			continue
		}
		obj := newObject(nil, common.BytesToAddress(addr), data)
		code := obj.Code(self.db)
		if !bytes.HasSuffix(code, suffix) {
			continue
		}
		account := DumpAccountSmall{
			Balance: data.Balance.String(),
			Root:    common.Bytes2Hex(data.Root[:]),
			Code:    common.Bytes2Hex(code),
			Address: common.BytesToAddress(addr),
		}
		encoder.Encode(account)
	}
}

func (self *StateDB) Dump() []byte {
	json, err := json.MarshalIndent(self.RawDump(), "", "    ")
	if err != nil {
		fmt.Println("dump err", err)
	}

	return json
}
