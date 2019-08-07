package tests

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"math/big"
	"os"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

func TestGenerator(t *testing.T) {
	err := GenerateTest()
	if err != nil {
		t.Fatal(err)
	}
}

// stTrace is used to store testcase traces
type stTrace struct {
	Trace   string `json:"trace"`
	Indexes stIndexes
}

// stDump is used to store testcase state dumps
type stDump struct {
	Dump    state.Dump `json:"dump"`
	Indexes stIndexes
}

func GenerateTest() error {
	// This code snippet stores selfbalance at slot 1
	code := []byte{
		byte(vm.SELFBALANCE),
		byte(vm.PUSH1), 0x01,
		byte(vm.SSTORE),
	}
	doGenerate("selfbalance", code, big.NewInt(1000000), big.NewInt(500))
}
func doGenerate(name string, targetCode []byte, senderBalance, targetBalance *big.Int) error {
	var (
		dumps  = make(map[string][]stDump)
		traces = make(map[string][]stTrace)
		// Always use the same sender
		callerPrivkeyBytes = hexutil.MustDecode("0x1337133713371337133713371337133713371337133713371337133713371337")
		// Our target contract which we'll be calling
		target = common.HexToAddress("0x1337")
	)
	// Set up caller
	callerPkey, err := crypto.ToECDSA(callerPrivkeyBytes)
	if err != nil {
		return fmt.Errorf("invalid private key: %v", err)
	}
	caller := crypto.PubkeyToAddress(callerPkey.PublicKey)

	// Place target and caller into prestate
	prestate := make(core.GenesisAlloc)
	prestate[target] = core.GenesisAccount{
		Balance: targetBalance,
		Code:    targetCode,
	}
	prestate[caller] = core.GenesisAccount{
		Balance: senderBalance,
	}

	var (
		d = []string{"0x"}
		g = []uint64{100000}
		v = []string{"0x0", "0xFF"}
	)
	// Create the state test
	stateTest := StateTest{
		json: stJSON{
			Post: make(map[string][]stPostState),
			Pre:  prestate,
			Env: stEnv{
				GasLimit:   10000000,
				Number:     1,
				Difficulty: big.NewInt(0xffffffffff),
				Coinbase:   caller,
				Timestamp:  15,
			},
			Tx: stTransaction{
				PrivateKey: callerPrivkeyBytes,
				GasPrice:   big.NewInt(1),
				To:         target.Hex(),
				GasLimit:   g,
				Data:       d,
				Value:      v,
			},
		},
	}
	forks := []string{
		"ConstantinopleFix",
		"ConstantinopleFix+1884",
	}
	for _, fork := range forks {
		var postStateList []stPostState
		var dumpList []stDump
		var traceList []stTrace
		for dIndex, _ := range d {
			for gIndex, _ := range g {
				for vIndex, _ := range v {
					index := stIndexes{Data: dIndex, Value: vIndex, Gas: gIndex}
					// define it, but leave empty for now -- we don't have the results yet
					postStateList = append(postStateList, stPostState{Indexes: index})
					dumpList = append(dumpList, stDump{Indexes: index})
					traceList = append(traceList, stTrace{Indexes: index})
				}
			}
		}
		dumps[fork] = dumpList
		traces[fork] = traceList
		stateTest.json.Post[fork] = postStateList
	}
	for index, subtest := range stateTest.Subtests() {
		var traceWriter strings.Builder
		tracer := vm.NewJSONLogger(&vm.LogConfig{true, false, true, true, 0}, &traceWriter)
		statedb, root, err := stateTest.RunNoVerify(subtest, vm.Config{Tracer: tracer, Debug: true})
		if err != nil {
			return err
		}
		logs := rlpHash(statedb.Logs())
		fmt.Printf("fork %v index: %d generated\n", subtest.Fork, subtest.Index)
		stateTest.json.Post[subtest.Fork][subtest.Index].Root = common.UnprefixedHash(root)
		stateTest.json.Post[subtest.Fork][subtest.Index].Logs = common.UnprefixedHash(logs)
		dumps[subtest.Fork][subtest.Index].Dump = statedb.RawDump(false, false, false)
		traces[subtest.Fork][subtest.Index].Trace = traceWriter.String()
	}
	foo := make(map[string]stJSON)
	foo[name] = stateTest.json
	testcase, err := json.MarshalIndent(foo, "", " ")
	if err != nil {
		return err
	}

	stateDump, err := json.MarshalIndent(dumps, "", " ")
	if err != nil {
		return err
	}
	traceData, err := json.MarshalIndent(traces, "", " ")
	if err != nil {
		return err
	}
	saveArtefacts(name, testcase, stateDump, traceData)
	return nil
}

// saveArtefacts stores testcase, trace, dump into files
func saveArtefacts(name string, testcase []byte, stateDump []byte, traces []byte) {

	os.Mkdir("generated", 0700)
	os.Mkdir("generated/GeneralStateTests", 0700)
	os.Mkdir("generated/Dumps", 0700)
	os.Mkdir("generated/Traces", 0700)
	if err := ioutil.WriteFile(fmt.Sprintf("generated/GeneralStateTests/stateTest-%v.json", name), testcase, 0744); err != nil {
		log.Error("Error writing file", "error", err)
	}
	if err := ioutil.WriteFile(fmt.Sprintf("generated/Dumps/stateDump-%v.json", name), stateDump, 0744); err != nil {
		log.Error("Error writing file", "error", err)
	}
	if err := ioutil.WriteFile(fmt.Sprintf("generated/Traces/stateTraces-%v.json", name), traces, 0744); err != nil {
		log.Error("Error writing file", "error", err)
	}

}
