package main

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/tests"
	"gopkg.in/urfave/cli.v1"
	"io/ioutil"
	"math/big"
	"strings"
)

// StateTest is a filled statetests
type StateTest struct {
	json stJSON
}

// StateSubtest is a particular subtest for a certain fork
type StateSubtest struct {
	Fork  string
	Index int
}

// StateTestFiller is a statetest which is not yet filled, that is, some fields are still not created
type StateTestFiller map[string]stFillerJSON

type stFillerJSON struct {
	Info   map[string]interface{} `json:"_info"`
	Env    stEnv                  `json:"env"`
	Expect []stExpect             `json:"expect"'`
	Pre    core.GenesisAlloc      `json:"pre"`
	Tx     stTransaction          `json:"transaction"`
}

type stExpect struct {
	Indexes map[string]int `json:"indexes"`
	Network []string       `json:"network"`
	Result  ExpectAlloc    `json:"result"`
}

//go:generate gencodec -type ExpectAccount -field-override expectAccountMarshaling -out gen_expect_account.go

// ExpectAlloc specifies the expected poststate for an account.
type ExpectAlloc map[common.Address]ExpectAccount

func (ga *ExpectAlloc) UnmarshalJSON(data []byte) error {
	m := make(map[common.UnprefixedAddress]ExpectAccount)
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	*ga = make(ExpectAlloc)
	for addr, a := range m {
		(*ga)[common.Address(addr)] = a
	}
	return nil
}

// ExpectAccount is an account in the expect-section of a statetest filler
type ExpectAccount struct {
	Code    []byte                      `json:"code,omitempty"`
	Storage map[common.Hash]common.Hash `json:"storage,omitempty"`
	Balance *big.Int                    `json:"balance"`
	Nonce   uint64                      `json:"nonce,omitempty"`
}

type expectAccountMarshaling struct {
	Code    hexutil.Bytes
	Balance *math.HexOrDecimal256
	Nonce   math.HexOrDecimal64
	Storage map[common.Hash]common.Hash
}

//go:generate gencodec -type stEnv -field-override stEnvMarshaling -out gen_stenv.go

type stEnv struct {
	Coinbase   common.Address `json:"currentCoinbase"   gencodec:"required"`
	Difficulty *big.Int       `json:"currentDifficulty" gencodec:"required"`
	GasLimit   uint64         `json:"currentGasLimit"   gencodec:"required"`
	Number     uint64         `json:"currentNumber"     gencodec:"required"`
	Timestamp  uint64         `json:"currentTimestamp"  gencodec:"required"`
}

type stEnvMarshaling struct {
	Coinbase   common.UnprefixedAddress
	Difficulty *math.HexOrDecimal256
	GasLimit   math.HexOrDecimal64
	Number     math.HexOrDecimal64
	Timestamp  math.HexOrDecimal64
}

//go:generate gencodec -type stTransaction -field-override stTransactionMarshaling -out gen_sttransaction.go

type stTransaction struct {
	GasPrice   *big.Int      `json:"gasPrice"`
	Nonce      uint64        `json:"nonce"`
	To         string        `json:"to"`
	Data       []string      `json:"data"`
	GasLimit   []uint64      `json:"gasLimit"`
	Value      []string      `json:"value"`
	PrivateKey UnprefixedKey `json:"secretKey"`
}

type stTransactionMarshaling struct {
	GasPrice   *math.HexOrDecimal256
	Nonce      math.HexOrDecimal64
	GasLimit   []math.HexOrDecimal64
	PrivateKey UnprefixedKey
}

// UnprefixedKey allows marshaling a key without 0x prefix.
type UnprefixedKey [32]byte

// UnmarshalText decodes the hash from hex. The 0x prefix is optional.
func (h *UnprefixedKey) UnmarshalText(input []byte) error {
	return hexutil.UnmarshalFixedUnprefixedText("UnprefixedKey", input, h[:])
}

// MarshalText encodes the hash as hex.
func (h UnprefixedKey) MarshalText() ([]byte, error) {
	return []byte(hex.EncodeToString(h[:])), nil
}

func (t *StateTest) UnmarshalJSON(in []byte) error {
	return json.Unmarshal(in, &t.json)
}

type stJSON struct {
	Env  stEnv                    `json:"env"`
	Pre  core.GenesisAlloc        `json:"pre"`
	Tx   stTransaction            `json:"transaction"`
	Out  hexutil.Bytes            `json:"out"`
	Post map[string][]stPostState `json:"post"`
}

type stPostState struct {
	Root    common.UnprefixedHash `json:"hash"`
	Logs    common.UnprefixedHash `json:"logs"`
	Indexes struct {
		Data  int `json:"data"`
		Gas   int `json:"gas"`
		Value int `json:"value"`
	}
}

func getConfig(networks []string) []*params.ChainConfig {

	var configs []*params.ChainConfig
	for _, network := range networks {
		if config, ok := tests.Forks[network]; ok {
			configs = append(configs, config)
		}
	}
	return configs
}
func dummyBlockHash(n uint64) common.Hash {
	return common.BytesToHash(crypto.Keccak256([]byte(big.NewInt(int64(n)).String())))
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

// Run executes a specific subtest.
func applyTxToPrestate(genesis *core.Genesis, msg core.Message) (*state.StateDB, common.Hash, common.Hash, error) {
	block := genesis.ToBlock(nil)
	chainConfig := genesis.Config
	statedb := tests.MakePreState(ethdb.NewMemDatabase(), genesis.Alloc)

	context := core.NewEVMContext(msg, block.Header(), nil, &genesis.Coinbase)
	context.GetHash = dummyBlockHash
	evm := vm.NewEVM(context, statedb, chainConfig, vm.Config{})

	gaspool := new(core.GasPool)
	gaspool.AddGas(block.GasLimit())
	snapshot := statedb.Snapshot()
	if _, _, _, err := core.ApplyMessage(evm, msg, gaspool); err != nil {
		statedb.RevertToSnapshot(snapshot)
	}
	// Commit block
	statedb.Commit(chainConfig.IsEIP158(block.Number()))
	// Add 0-value mining reward. This only makes a difference in the cases
	// where
	// - the coinbase suicided, or
	// - there are only 'bad' transactions, which aren't executed. In those cases,
	//   the coinbase gets no txfee, so isn't created, and thus needs to be touched
	statedb.AddBalance(block.Coinbase(), new(big.Int))
	// And _now_ get the state root
	root := statedb.IntermediateRoot(chainConfig.IsEIP158(block.Number()))
	// N.B: We need to do this in a two-step process, because the first Commit takes care
	// of suicides, and we need to touch the coinbase _after_ it has potentially suicided.

	logs := rlpHash(statedb.Logs())

	return statedb, root, logs, nil
}

type txIndex struct {
	msg           core.Message
	dataIndex     int
	valueIndex    int
	gasLimitIndex int
}

func (tx *stTransaction) getAllTxs() ([]*txIndex, error) {
	// Derive sender from private key if present.
	var from common.Address
	if len(tx.PrivateKey) > 0 {
		key, err := crypto.ToECDSA(tx.PrivateKey[:])
		if err != nil {
			return nil, fmt.Errorf("invalid private key: %v", err)
		}
		from = crypto.PubkeyToAddress(key.PublicKey)
	}
	// Parse recipient if present.
	var to *common.Address
	if tx.To != "" {
		to = new(common.Address)
		if err := to.UnmarshalText([]byte(tx.To)); err != nil {
			return nil, fmt.Errorf("invalid to address: %v", err)
		}
	}
	var txs []*txIndex
	for di, d := range tx.Data {
		data, err := hex.DecodeString(strings.TrimPrefix(d, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid tx data (index %d): %v ", di, d)
		}
		for vi, v := range tx.Value {
			value := new(big.Int)
			if v != "0x" {
				var ok bool
				if value, ok = math.ParseBig256(v); !ok {
					return nil, fmt.Errorf("invalid tx value (index %d) %v", vi, v)
				}
			}
			for gli, gl := range tx.GasLimit {
				msg := types.NewMessage(from, to, tx.Nonce, value, gl, tx.GasPrice, data, true)
				txs = append(txs, &txIndex{
					dataIndex:     di,
					gasLimitIndex: gli,
					valueIndex:    vi,
					msg:           msg,
				})
			}
		}
	}
	return txs, nil
}

func convertCmd(ctx *cli.Context) error {
	if !ctx.GlobalIsSet(FromFileFlag.Name) {
		return fmt.Errorf("from file not set")
	}
	src, err := ioutil.ReadFile(ctx.GlobalString(FromFileFlag.Name))
	if err != nil {
		return err
	}
	var tests = StateTestFiller{}
	if err = json.Unmarshal(src, &tests); err != nil {
		return err
	}

	for name, test := range tests {
		fmt.Printf("Test %v\n", name)
		for _, e := range test.Expect {
			fmt.Printf("Networks: %v\n", e.Network)
			confs := getConfig(e.Network)
			for _, config := range confs {
				genesis := &core.Genesis{
					Config:     config,
					Coinbase:   test.Env.Coinbase,
					Difficulty: test.Env.Difficulty,
					GasLimit:   test.Env.GasLimit,
					Number:     test.Env.Number,
					Timestamp:  test.Env.Timestamp,
					Alloc:      test.Pre,
				}
				txs, err := test.Tx.getAllTxs()
				if err != nil {
					return err
				}
				for _, msgindex := range txs {
					_, root, logs, _ := applyTxToPrestate(genesis, msgindex.msg)
					fmt.Printf("d %d, g %v, v %d\n\troot %x \n\tlogs %x \n",
						msgindex.dataIndex,
						msgindex.gasLimitIndex,
						msgindex.valueIndex,
						root,
						logs)
				}
			}
		}
	}
	if !ctx.GlobalIsSet(FromFormatFlag.Name) {
		return fmt.Errorf("from format not set")
	}
	if !ctx.GlobalIsSet(ToFileFlag.Name) {
		return fmt.Errorf("to file not set")
	}
	if !ctx.GlobalIsSet(ToFormatFlag.Name) {
		return fmt.Errorf("to format not set")
	}

	return nil
}
