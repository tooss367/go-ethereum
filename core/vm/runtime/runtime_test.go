// Copyright 2015 The go-ethereum Authors
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

package runtime

import (
	"bytes"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
)

func TestDefaults(t *testing.T) {
	cfg := new(Config)
	setDefaults(cfg)

	if cfg.Difficulty == nil {
		t.Error("expected difficulty to be non nil")
	}

	if cfg.Time == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.GasLimit == 0 {
		t.Error("didn't expect gaslimit to be zero")
	}
	if cfg.GasPrice == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.Value == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.GetHashFn == nil {
		t.Error("expected time to be non nil")
	}
	if cfg.BlockNumber == nil {
		t.Error("expected block number to be non nil")
	}
}

func TestEVM(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("crashed with: %v", r)
		}
	}()

	Execute([]byte{
		byte(vm.DIFFICULTY),
		byte(vm.TIMESTAMP),
		byte(vm.GASLIMIT),
		byte(vm.PUSH1),
		byte(vm.ORIGIN),
		byte(vm.BLOCKHASH),
		byte(vm.COINBASE),
	}, nil, nil)
}

func TestExecute(t *testing.T) {
	ret, _, err := Execute([]byte{
		byte(vm.PUSH1), 10,
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	}, nil, nil)
	if err != nil {
		t.Fatal("didn't expect error", err)
	}

	num := new(big.Int).SetBytes(ret)
	if num.Cmp(big.NewInt(10)) != 0 {
		t.Error("Expected 10, got", num)
	}
}

func TestCall(t *testing.T) {
	state, _ := state.New(common.Hash{}, state.NewDatabase(ethdb.NewMemDatabase()))
	address := common.HexToAddress("0x0a")
	state.SetCode(address, []byte{
		byte(vm.PUSH1), 10,
		byte(vm.PUSH1), 0,
		byte(vm.MSTORE),
		byte(vm.PUSH1), 32,
		byte(vm.PUSH1), 0,
		byte(vm.RETURN),
	})

	ret, _, err := Call(address, nil, &Config{State: state})
	if err != nil {
		t.Fatal("didn't expect error", err)
	}

	num := new(big.Int).SetBytes(ret)
	if num.Cmp(big.NewInt(10)) != 0 {
		t.Error("Expected 10, got", num)
	}
}

func BenchmarkCall(b *testing.B) {
	var definition = `[{"constant":true,"inputs":[],"name":"seller","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":false,"inputs":[],"name":"abort","outputs":[],"type":"function"},{"constant":true,"inputs":[],"name":"value","outputs":[{"name":"","type":"uint256"}],"type":"function"},{"constant":false,"inputs":[],"name":"refund","outputs":[],"type":"function"},{"constant":true,"inputs":[],"name":"buyer","outputs":[{"name":"","type":"address"}],"type":"function"},{"constant":false,"inputs":[],"name":"confirmReceived","outputs":[],"type":"function"},{"constant":true,"inputs":[],"name":"state","outputs":[{"name":"","type":"uint8"}],"type":"function"},{"constant":false,"inputs":[],"name":"confirmPurchase","outputs":[],"type":"function"},{"inputs":[],"type":"constructor"},{"anonymous":false,"inputs":[],"name":"Aborted","type":"event"},{"anonymous":false,"inputs":[],"name":"PurchaseConfirmed","type":"event"},{"anonymous":false,"inputs":[],"name":"ItemReceived","type":"event"},{"anonymous":false,"inputs":[],"name":"Refunded","type":"event"}]`

	var code = common.Hex2Bytes("6060604052361561006c5760e060020a600035046308551a53811461007457806335a063b4146100865780633fa4f245146100a6578063590e1ae3146100af5780637150d8ae146100cf57806373fac6f0146100e1578063c19d93fb146100fe578063d696069714610112575b610131610002565b610133600154600160a060020a031681565b610131600154600160a060020a0390811633919091161461015057610002565b61014660005481565b610131600154600160a060020a039081163391909116146102d557610002565b610133600254600160a060020a031681565b610131600254600160a060020a0333811691161461023757610002565b61014660025460ff60a060020a9091041681565b61013160025460009060ff60a060020a9091041681146101cc57610002565b005b600160a060020a03166060908152602090f35b6060908152602090f35b60025460009060a060020a900460ff16811461016b57610002565b600154600160a060020a03908116908290301631606082818181858883f150506002805460a060020a60ff02191660a160020a179055506040517f72c874aeff0b183a56e2b79c71b46e1aed4dee5e09862134b8821ba2fddbf8bf9250a150565b80546002023414806101dd57610002565b6002805460a060020a60ff021973ffffffffffffffffffffffffffffffffffffffff1990911633171660a060020a1790557fd5d55c8a68912e9a110618df8d5e2e83b8d83211c57a8ddd1203df92885dc881826060a15050565b60025460019060a060020a900460ff16811461025257610002565b60025460008054600160a060020a0390921691606082818181858883f150508354604051600160a060020a0391821694503090911631915082818181858883f150506002805460a060020a60ff02191660a160020a179055506040517fe89152acd703c9d8c7d28829d443260b411454d45394e7995815140c8cbcbcf79250a150565b60025460019060a060020a900460ff1681146102f057610002565b6002805460008054600160a060020a0390921692909102606082818181858883f150508354604051600160a060020a0391821694503090911631915082818181858883f150506002805460a060020a60ff02191660a160020a179055506040517f8616bbbbad963e4e65b1366f1d75dfb63f9e9704bbbf91fb01bec70849906cf79250a15056")

	abi, err := abi.JSON(strings.NewReader(definition))
	if err != nil {
		b.Fatal(err)
	}

	cpurchase, err := abi.Pack("confirmPurchase")
	if err != nil {
		b.Fatal(err)
	}
	creceived, err := abi.Pack("confirmReceived")
	if err != nil {
		b.Fatal(err)
	}
	refund, err := abi.Pack("refund")
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for j := 0; j < 400; j++ {
			Execute(code, cpurchase, nil)
			Execute(code, creceived, nil)
			Execute(code, refund, nil)
		}
	}
}
func benchmarkEVM_Create(bench *testing.B, code string) {
	var (
		statedb, _ = state.New(common.Hash{}, state.NewDatabase(ethdb.NewMemDatabase()))
		sender     = common.BytesToAddress([]byte("sender"))
		receiver   = common.BytesToAddress([]byte("receiver"))
	)

	statedb.CreateAccount(sender)
	statedb.SetCode(receiver, common.FromHex(code))
	runtimeConfig := Config{
		Origin:      sender,
		State:       statedb,
		GasLimit:    10000000,
		Difficulty:  big.NewInt(0x200000),
		Time:        new(big.Int).SetUint64(0),
		Coinbase:    common.Address{},
		BlockNumber: new(big.Int).SetUint64(1),
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      new(big.Int),
			ByzantiumBlock:      new(big.Int),
			ConstantinopleBlock: new(big.Int),
			DAOForkBlock:        new(big.Int),
			DAOForkSupport:      false,
			EIP150Block:         new(big.Int),
			EIP155Block:         new(big.Int),
			EIP158Block:         new(big.Int),
		},
		EVMConfig: vm.Config{},
	}
	// Warm up the intpools and stuff
	bench.ResetTimer()
	for i := 0; i < bench.N; i++ {
		Call(receiver, []byte{}, &runtimeConfig)
	}
	bench.StopTimer()
}

func BenchmarkEVM_CREATE_500(bench *testing.B) {
	// initcode size 500K, repeatedly calls CREATE and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b6207a120600080f0600152600056")
}
func BenchmarkEVM_CREATE2_500(bench *testing.B) {
	// initcode size 500K, repeatedly calls CREATE2 and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b586207a120600080f5600152600056")
}
func BenchmarkEVM_CREATE_1200(bench *testing.B) {
	// initcode size 1200K, repeatedly calls CREATE and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b62124f80600080f0600152600056")
}
func BenchmarkEVM_CREATE2_1200(bench *testing.B) {
	// initcode size 1200K, repeatedly calls CREATE2 and then modifies the mem contents
	benchmarkEVM_Create(bench, "5b5862124f80600080f5600152600056")
}

type dummyTracer struct {
	startgas uint64
}

func (d *dummyTracer) CaptureStart(from common.Address, to common.Address, call bool, input []byte, gas uint64, value *big.Int) error {
	return nil
}

func (d *dummyTracer) CaptureState(env *vm.EVM, pc uint64, op vm.OpCode, gas, cost uint64, memory *vm.Memory, stack *vm.Stack, contract *vm.Contract, depth int, err error) error {
	fmt.Printf("pc %x op: %v gas %d (%d), stack %v, cost %d\n", pc, op.String(), gas, d.startgas-gas, stack.Data(), cost)
	return nil
}

func (d *dummyTracer) CaptureFault(env *vm.EVM, pc uint64, op vm.OpCode, gas, cost uint64, memory *vm.Memory, stack *vm.Stack, contract *vm.Contract, depth int, err error) error {
	fmt.Printf("err %v\n", err)
	return nil
}

func (d *dummyTracer) CaptureEnd(output []byte, gasUsed uint64, t time.Duration, err error) error {
	fmt.Printf("end: gasUsed %d\n", gasUsed)
	return nil
}

func TestConstantinopleSuicideAndCodehash(t *testing.T) {
	var (
		statedb, _     = state.New(common.Hash{}, state.NewDatabase(ethdb.NewMemDatabase()))
		sender         = common.BytesToAddress(common.FromHex("a94f5374fce5edbc8e2a8697c15331677e6ebf0b"))
		receiver       = common.BytesToAddress(common.FromHex("1000000000000000000000000000000000000000"))
		selfdestructor = common.BytesToAddress(common.FromHex("dead"))
	)

	statedb.CreateAccount(sender)
	// The flow of this test is
	// 1. receiver does a 0-value CALL to 'selfdestructor', who
	//   1.1. does selfdestruct(0xabcdef)
	// 2. receiver then checks extcodehash(0xabcdef)
	// The expected returnvalue is `0x0`, not empty codehash
	code := "6000600060006000600061dead5a" + // push call args, ending with 0xdead address and GAS
		"f1" + // CALL
		"62abcdef" + // push3 0xabcdef
		"3f" + // extcodehash
		"600052" + // store result in memory location 1
		"60206000" + // push size 32 offset 0
		"f3" // return

	statedb.SetCode(receiver, common.FromHex(code))
	statedb.SetBalance(receiver, big.NewInt(1))

	selfdestructorcode := []byte{
		byte(vm.PUSH3), 0xAB, 0xCD, 0xEF,
		byte(vm.SELFDESTRUCT),
	}

	statedb.SetCode(selfdestructor, selfdestructorcode)
	runtimeConfig := Config{
		Origin:      sender,
		State:       statedb,
		GasLimit:    10000000,
		Difficulty:  big.NewInt(0x200000),
		Time:        new(big.Int).SetUint64(0),
		Coinbase:    common.Address{},
		BlockNumber: new(big.Int).SetUint64(1),
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      new(big.Int),
			ByzantiumBlock:      new(big.Int),
			ConstantinopleBlock: new(big.Int),
			DAOForkBlock:        new(big.Int),
			DAOForkSupport:      false,
			EIP150Block:         new(big.Int),
			EIP155Block:         new(big.Int),
			EIP158Block:         new(big.Int),
		},
		EVMConfig: vm.Config{},
	}
	expected := common.FromHex("0000000000000000000000000000000000000000000000000000000000000000")
	ret, _, err := Call(receiver, []byte{}, &runtimeConfig)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	if len(ret) != 32 {
		t.Errorf("expected return length 32, got %d: %x ", len(ret), ret)
		return
	}
	if bytes.Compare(ret, expected) != 0 {
		t.Errorf("expected %v got %v", hexutil.Encode(expected), hexutil.Encode(ret))
	}
}

func TestConstantinopleSuicideAndNonzerovalueCall(t *testing.T) {
	var (
		statedb, _     = state.New(common.Hash{}, state.NewDatabase(ethdb.NewMemDatabase()))
		sender         = common.BytesToAddress(common.FromHex("a94f5374fce5edbc8e2a8697c15331677e6ebf0b"))
		receiver       = common.BytesToAddress(common.FromHex("1000000000000000000000000000000000000000"))
		selfdestructor = common.BytesToAddress(common.FromHex("dead"))
		// Flip this to get a trace on stdout
		printTrace = false
	)

	statedb.CreateAccount(sender)
	// The flow of this test is
	// 1. receiver does a 0-value CALL to 'selfdestructor', who
	//   1.1. does selfdestruct(0xabcdef)
	// 2. receiver then does a 1-wei call to 0xabcdef
	// The call should cost params.CallNewAccountGas (25K) more than if the account
	// had not existed

	code := "6000600060006000600061dead5a" + // push call args, ending with 0xdead address and GAS
		"f1" + // CALL
		"62abcdef" + // push3 0xabcdef
		"6000600060006000" + // zeroes
		"6001" + "62abcdef" + "6000" + // value, address, gas
		"f1" + // CALL
		"5a" + // GAS
		"600052" + // store result in memory location 1
		"60206000" + // push size 32 offset 0
		"f3" // return

	statedb.SetCode(receiver, common.FromHex(code))
	statedb.SetBalance(receiver, big.NewInt(1))

	selfdestructorcode := []byte{
		byte(vm.PUSH3), 0xAB, 0xCD, 0xEF,
		byte(vm.SELFDESTRUCT),
	}
	vmconf := vm.Config{}
	if printTrace {
		vmconf = vm.Config{
			Tracer: &dummyTracer{10000000},
			Debug:  true,
		}
	}
	statedb.SetCode(selfdestructor, selfdestructorcode)
	runtimeConfig := Config{
		Origin:      sender,
		State:       statedb,
		GasLimit:    10000000,
		Difficulty:  big.NewInt(0x200000),
		Time:        new(big.Int).SetUint64(0),
		Coinbase:    common.Address{},
		BlockNumber: new(big.Int).SetUint64(1),
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      new(big.Int),
			ByzantiumBlock:      new(big.Int),
			ConstantinopleBlock: new(big.Int),
			DAOForkBlock:        new(big.Int),
			DAOForkSupport:      false,
			EIP150Block:         new(big.Int),
			EIP155Block:         new(big.Int),
			EIP158Block:         new(big.Int),
		},
		EVMConfig: vmconf,
	}
	expected := common.FromHex("0x000000000000000000000000000000000000000000000000000000000098017b")
	ret, leftovergas, err := Call(receiver, []byte{}, &runtimeConfig)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	if bytes.Compare(ret, expected) != 0 {
		t.Errorf("expected %v got %v", hexutil.Encode(expected), hexutil.Encode(ret))
	}
	gasUsed := 10000000 - leftovergas
	// Gas
	// 700 first call
	// 5k suicide            5700
	// 700 second call
	// 25K CallNewAccountGas
	// 9K for value-transfer,
	// - 2300, the gas stipend that is not used
	// 700+5000+700+25000+9000-2300 = 38100, plus some for other ops
	if gasUsed != 38164 {
		t.Errorf("expected return gasUsed, got %d: %x ", gasUsed, 38164)
		return
	}

}

func TestConstantinopleExtcodeCopyAndExtcodeHash(t *testing.T) {
	var (
		statedb, _     = state.New(common.Hash{}, state.NewDatabase(ethdb.NewMemDatabase()))
		sender         = common.BytesToAddress(common.FromHex("a94f5374fce5edbc8e2a8697c15331677e6ebf0b"))
		receiver       = common.BytesToAddress(common.FromHex("1000000000000000000000000000000000000000"))
		selfdestructor = common.BytesToAddress(common.FromHex("dead"))
		// Flip this to get a trace on stdout
		printTrace = false
	)

	statedb.CreateAccount(sender)
	// The flow of this test is
	// 1. receiver then EXTCODEHASH on non-existing 0xabcdef
	// 1. receiver does EXTCODECOPY on non-existing 0xabcdef
	// 2. receiver then EXTCODEHASH on non-existing 0xabcdef
	// The call should cost params.CallNewAccountGas (25K) more than if the account
	// had not existed

	code := "62abcdef3f" + // push 0xabcef, EXTCODEHASH
		// length, codeoffset, memoffset, addr, EXTCODEHASH
		"6001" + "6000" + "6000" + "62abcdef" + "3c" +
		"62abcdef3f" + // push 0xabcef, EXTCODEHASH
		"600052" + // store result in memory location 1
		"60206000" + // push size 32 offset 0
		"f3" // return
	statedb.SetCode(receiver, common.FromHex(code))
	statedb.SetBalance(receiver, big.NewInt(1))

	selfdestructorcode := []byte{
		byte(vm.PUSH3), 0xAB, 0xCD, 0xEF,
		byte(vm.SELFDESTRUCT),
	}
	vmconf := vm.Config{}
	if printTrace {
		vmconf = vm.Config{
			Tracer: &dummyTracer{10000000},
			Debug:  true,
		}
	}
	statedb.SetCode(selfdestructor, selfdestructorcode)
	runtimeConfig := Config{
		Origin:      sender,
		State:       statedb,
		GasLimit:    10000000,
		Difficulty:  big.NewInt(0x200000),
		Time:        new(big.Int).SetUint64(0),
		Coinbase:    common.Address{},
		BlockNumber: new(big.Int).SetUint64(1),
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      new(big.Int),
			ByzantiumBlock:      new(big.Int),
			ConstantinopleBlock: new(big.Int),
			DAOForkBlock:        new(big.Int),
			DAOForkSupport:      false,
			EIP150Block:         new(big.Int),
			EIP155Block:         new(big.Int),
			EIP158Block:         new(big.Int),
		},
		EVMConfig: vmconf,
	}
	expected := common.FromHex("0x0000000000000000000000000000000000000000000000000000000000000000")
	ret, _, err := Call(receiver, []byte{}, &runtimeConfig)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	if bytes.Compare(ret, expected) != 0 {
		t.Errorf("expected %v got %v", hexutil.Encode(expected), hexutil.Encode(ret))
	}
}

func TestConstantinopleZerovaluecallAndExtcodeHash(t *testing.T) {
	var (
		statedb, _     = state.New(common.Hash{}, state.NewDatabase(ethdb.NewMemDatabase()))
		sender         = common.BytesToAddress(common.FromHex("a94f5374fce5edbc8e2a8697c15331677e6ebf0b"))
		receiver       = common.BytesToAddress(common.FromHex("1000000000000000000000000000000000000000"))
		selfdestructor = common.BytesToAddress(common.FromHex("dead"))
		// Flip this to get a trace on stdout
		printTrace = true
	)

	statedb.CreateAccount(sender)
	// The flow of this test is
	// 1. receiver does 0-value call to 0xabcdef
	// 2. receiver does EXTCODEHASH on non-existing 0xabcdef
	// 3. receiver does 0-value call to 0xabcdef
	// 4. receiver does EXTCODEHASH on non-existing 0xabcdef
	// The expected returnvalue is all zeroes
	code := "" +
		// four zeroes, then , value, addr, GAS,,  call
		"6000" + "6000" + "6000" + "6000" + "6000" + "62abcdef" + "5a" + "f1" +
		"62abcdef3f" + // push 0xabcef, EXTCODEHASH
		// four zeroes, then , value, addr, GAS,,  call

		"6000" + "6000" + "6000" + "6000" + "6000" + "62abcdef" + "5a" + "f1" +
		"62abcdef3f" + // push 0xabcef, EXTCODEHASH
		"600052" + // store result in memory location 1
		"60206000" + // push size 32 offset 0
		"f3" // return
	statedb.SetCode(receiver, common.FromHex(code))
	statedb.SetBalance(receiver, big.NewInt(1))

	selfdestructorcode := []byte{
		byte(vm.PUSH3), 0xAB, 0xCD, 0xEF,
		byte(vm.SELFDESTRUCT),
	}
	vmconf := vm.Config{}
	if printTrace {
		vmconf = vm.Config{
			Tracer: &dummyTracer{10000000},
			Debug:  true,
		}
	}
	statedb.SetCode(selfdestructor, selfdestructorcode)
	runtimeConfig := Config{
		Origin:      sender,
		State:       statedb,
		GasLimit:    10000000,
		Difficulty:  big.NewInt(0x200000),
		Time:        new(big.Int).SetUint64(0),
		Coinbase:    common.Address{},
		BlockNumber: new(big.Int).SetUint64(1),
		ChainConfig: &params.ChainConfig{
			ChainID:             big.NewInt(1),
			HomesteadBlock:      new(big.Int),
			ByzantiumBlock:      new(big.Int),
			ConstantinopleBlock: new(big.Int),
			DAOForkBlock:        new(big.Int),
			DAOForkSupport:      false,
			EIP150Block:         new(big.Int),
			EIP155Block:         new(big.Int),
			EIP158Block:         new(big.Int),
		},
		EVMConfig: vmconf,
	}
	expected := common.FromHex("0x0000000000000000000000000000000000000000000000000000000000000000")
	ret, _, err := Call(receiver, []byte{}, &runtimeConfig)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
		return
	}
	if bytes.Compare(ret, expected) != 0 {
		t.Errorf("expected %v got %v", hexutil.Encode(expected), hexutil.Encode(ret))
	}
}
