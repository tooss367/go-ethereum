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

package core

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

type errorWrap struct {
	errString string
}

type generateFn func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database)

func getErrChain(t *testing.T, generate generateFn, wrap *errorWrap) {
	t.Helper()
	var (
		db          = rawdb.NewMemoryDatabase()
		genesis     = new(Genesis).MustCommit(db)
		engine      = ethash.NewFaker()
		chainConfig = &params.ChainConfig{
			ChainID:        big.NewInt(1337),
			HomesteadBlock: big.NewInt(0),
			EIP150Block:    big.NewInt(0),
			EIP155Block:    big.NewInt(0),
			EIP158Block:    big.NewInt(0),
			ByzantiumBlock: big.NewInt(5),
			Ethash:         new(params.EthashConfig),
		}
	)
	// The chainmaker 'panic's on these type of errors, so we need this
	// little wrapper here
	defer func() {
		if r := recover(); r != nil {
			wrap.errString = fmt.Sprintf("%v", r)
		}
	}()
	generate(chainConfig, genesis, engine, db)
}

// TestStateProcessorErrors tests the output from the 'core' errors as defined in
// core/error.go
func TestStateProcessorErrors(t *testing.T) {
	signer := types.HomesteadSigner{}
	testKey, _ := crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	{
		want := "nonce too low: address 0x71562b71999873db5b286df957af199ec94617f7, tx: 0 state: 1"
		wrap := &errorWrap{"no error"}
		getErrChain(t, func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database) {
			GenerateChain(chainConfig, genesis, engine, db, 1, func(i int, b *BlockGen) {
				tx1, _ := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), params.TxGas, nil, nil), signer, testKey)
				tx2, _ := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), params.TxGas, nil, nil), signer, testKey)
				b.AddTx(tx1)
				b.AddTx(tx2)
			})
		}, wrap)
		if have := wrap.errString; have != want {
			t.Errorf("have\n'%v'\nwant\n'%v'", have, want)
		}
	}
	{
		want := "nonce too high: address 0x71562b71999873db5b286df957af199ec94617f7, tx: 100 state: 0"
		wrap := &errorWrap{"no error"}
		getErrChain(t, func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database) {
			GenerateChain(chainConfig, genesis, engine, db, 1, func(i int, b *BlockGen) {
				tx, _ := types.SignTx(types.NewTransaction(100, common.Address{}, big.NewInt(0), params.TxGas, nil, nil), signer, testKey)
				b.AddTx(tx)
			})
		}, wrap)
		if have := wrap.errString; have != want {
			t.Errorf("have\n'%v'\nwant\n'%v'", have, want)
		}
	}
	{
		want := "gas limit reached"
		wrap := &errorWrap{"no error"}
		getErrChain(t, func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database) {
			GenerateChain(chainConfig, genesis, engine, db, 1, func(i int, b *BlockGen) {
				tx, _ := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), 21000000, nil, nil), signer, testKey)
				b.AddTx(tx)
			})
		}, wrap)
		if have := wrap.errString; have != want {
			t.Errorf("have\n'%v'\nwant\n'%v'", have, want)
		}
	}
	{
		want := "insufficient funds for transfer: address 0x71562b71999873db5b286df957af199ec94617f7"
		wrap := &errorWrap{"no error"}
		getErrChain(t, func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database) {
			GenerateChain(chainConfig, genesis, engine, db, 1, func(i int, b *BlockGen) {
				tx, _ := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0xffffffffffff), params.TxGas, nil, nil), signer, testKey)
				b.AddTx(tx)
			})
		}, wrap)
		if have := wrap.errString; have != want {
			t.Errorf("have\n'%v'\nwant\n'%v'", have, want)
		}
	}
	{
		want := "insufficient funds for gas * price + value: address 0x71562b71999873db5b286df957af199ec94617f7 have 0 want 352321515000"
		wrap := &errorWrap{"no error"}
		getErrChain(t, func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database) {
			GenerateChain(chainConfig, genesis, engine, db, 1, func(i int, b *BlockGen) {
				tx, _ := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), params.TxGas, big.NewInt(0xffffff), nil), signer, testKey)
				b.AddTx(tx)
			})
		}, wrap)
		if have := wrap.errString; have != want {
			t.Errorf("have\n'%v'\nwant\n'%v'", have, want)
		}
	}
	{
		want := "intrinsic gas too low: have 20000, want 21000"
		wrap := &errorWrap{"no error"}
		getErrChain(t, func(chainConfig *params.ChainConfig, genesis *types.Block, engine consensus.Engine, db ethdb.Database) {
			GenerateChain(chainConfig, genesis, engine, db, 1, func(i int, b *BlockGen) {
				tx, _ := types.SignTx(types.NewTransaction(0, common.Address{}, big.NewInt(0), params.TxGas-1000, big.NewInt(0), nil), signer, testKey)
				b.AddTx(tx)
			})
		}, wrap)
		if have := wrap.errString; have != want {
			t.Errorf("have\n'%v'\nwant\n'%v'", have, want)
		}
	}
	{
		// The last 'core' error is ErrGasUintOverflow: "gas uint64 overflow", but in order to
		// trigger that one, we'd have to allocate a _huge_ chunk of data, such that the
		// multiplication len(data) +gas_per_byte overflows uint64. Not testable at the moment
	}
}
