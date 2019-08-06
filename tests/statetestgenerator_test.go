package tests

import (
	"fmt"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
)

func TestGenerator(t *testing.T) {
	err := GenerateTest()
	if err != nil {
		t.Fatal(err)
	}
}

func GenerateTest() error {
	// Set up caller
	//callerPrivkeyBytes, err :=
	callerPkey, err := crypto.HexToECDSA("1337133713371337133713371337133713371337133713371337133713371337")
	if err != nil {
		return fmt.Errorf("invalid private key: %v", err)
	}
	caller := crypto.PubkeyToAddress(callerPkey.PublicKey)
	coinbase := common.HexToAddress("0xba5e")

	// Our target contract which we'll be calling
	target := common.HexToAddress("0x1337")
	msg := types.NewMessage(caller, &target, 0, big.NewInt(0), 100000, big.NewInt(1), nil, true)

	// This code snippet stores selfbalance at slot 1
	code := []byte{
		byte(vm.SELFBALANCE),
		byte(vm.PUSH1), 0x01,
		byte(vm.SSTORE),
	}
	// Place target and caller into prestate
	prestate := make(core.GenesisAlloc)
	prestate[target] = core.GenesisAccount{
		Balance: big.NewInt(500),
		Code:    code,
	}

	prestate[caller] = core.GenesisAccount{
		Balance: big.NewInt(1000000),
	}

	name := "ConstantinopleFix+1884"
	// Copy pasta fron state_test_util
	config, eips, err := getVMConfig(name)
	if err != nil {
		return UnsupportedForkError{name}
	}
	vmconfig := vm.Config{
		ExtraEips: eips,
	}
	genesis := &core.Genesis{
		Config:     config,
		Coinbase:   coinbase,
		Difficulty: big.NewInt(0xffffffffff),
		GasLimit:   10000000,
		Number:     1,
		Timestamp:  15,
		Alloc:      nil,
	}
	block := genesis.ToBlock(nil)
	statedb := MakePreState(rawdb.NewMemoryDatabase(), prestate)
	context := core.NewEVMContext(msg, block.Header(), nil, &coinbase)
	context.GetHash = vmTestBlockHash
	evm := vm.NewEVM(context, statedb, config, vmconfig)

	gaspool := new(core.GasPool)
	gaspool.AddGas(block.GasLimit())
	snapshot := statedb.Snapshot()
	if _, _, _, err := core.ApplyMessage(evm, msg, gaspool); err != nil {
		statedb.RevertToSnapshot(snapshot)
	}
	// Commit block
	statedb.Commit(config.IsEIP158(block.Number()))
	// Add 0-value mining reward. This only makes a difference in the cases
	// where
	// - the coinbase suicided, or
	// - there are only 'bad' transactions, which aren't executed. In those cases,
	//   the coinbase gets no txfee, so isn't created, and thus needs to be touched
	statedb.AddBalance(block.Coinbase(), new(big.Int))
	// And _now_ get the state root
	root := statedb.IntermediateRoot(config.IsEIP158(block.Number()))
	fmt.Printf("root : 0x%x\n", root)
	logs := rlpHash(statedb.Logs())
	fmt.Printf("logs : 0x%x\n", logs)
	outp := statedb.Dump(false, false, false)
	fmt.Printf("State %s", outp)
	return nil
}
