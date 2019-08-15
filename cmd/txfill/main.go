package main

import (
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/params"
	"io/ioutil"
	"math/big"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

var (
	// Git SHA1 commit hash of the release (set via linker flags)
	gitCommit = ""
	gitDate   = ""
)

type txFiller struct {
	Expect      []expectSection `json:"expect"`
	Transaction txFromFiller    `json:"transaction"`
}

type expectSection struct {
	Network []string `json:"network"`
	Result  string   `json: "result"`
	Sender  string   `json:"sender"`
}

// AddressOrNil allows marshaling an Address with or without 0x prefix, and
// optionally nil
type AddressOrNil common.Address

// UnmarshalText decodes the address from hex. The 0x prefix is optional.
func (a *AddressOrNil) UnmarshalText(input []byte) error {
	if len(input) == 0 {
		return nil
	}
	return hexutil.UnmarshalFixedUnprefixedText("UnprefixedAddress", input, a[:])
}

// HexOrDecimalUnlimited marshals big.Int as hex or decimal.
type HexOrDecimalUnlimited big.Int

// UnmarshalText implements encoding.TextUnmarshaler.
func (i *HexOrDecimalUnlimited) UnmarshalText(input []byte) error {
	bigint, ok := parseBigUnlimited(string(input))
	if !ok {
		return fmt.Errorf("invalid hex or decimal integer %q", input)
	}
	*i = HexOrDecimalUnlimited(*bigint)
	return nil
}

// MarshalText implements encoding.TextMarshaler.
func (i *HexOrDecimalUnlimited) MarshalText() ([]byte, error) {
	if i == nil {
		return []byte("0x0"), nil
	}
	return []byte(fmt.Sprintf("%#x", (*big.Int)(i))), nil
}

// parseBigUnlimited parses s as an unlimited bit integer in decimal or hexadecimal syntax.
// Leading zeros are accepted. The empty string parses as zero.
func parseBigUnlimited(s string) (*big.Int, bool) {
	if s == "" {
		return new(big.Int), true
	}
	var bigint *big.Int
	var ok bool
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		bigint, ok = new(big.Int).SetString(s[2:], 16)
	} else {
		bigint, ok = new(big.Int).SetString(s, 10)
	}
	return bigint, ok
}

// txFromFiller is helper to read filler json
type txFromFiller struct {
	AccountNonce HexOrDecimalUnlimited `json:"nonce" `
	Price        HexOrDecimalUnlimited `json:"gasPrice"`
	GasLimit     HexOrDecimalUnlimited `json:"gasLimit"`
	Recipient    string                `json:"to"` // nil means contract creation
	Amount       HexOrDecimalUnlimited `json:"value"`
	Payload      hexutil.Bytes         `json:"data"`
	V            HexOrDecimalUnlimited `json:"v"`
	R            HexOrDecimalUnlimited `json:"r"`
	S            HexOrDecimalUnlimited `json:"s"`
}

// txForRlp is a helper to output RLP
type txForRlp struct {
	AccountNonce big.Int
	Price        big.Int
	GasLimit     big.Int
	Recipient []byte `rlp:"nil"`
	Amount    big.Int
	Payload   hexutil.Bytes
	V         big.Int
	R         big.Int
	S         big.Int
}


// rlpTx takes a transaction from the filler and produces the RLP for it
func rlpTx(tx txFromFiller) ([]byte, error) {

	marshaller := txForRlp{
		GasLimit:     big.Int(tx.GasLimit),
		AccountNonce: big.Int(tx.AccountNonce),
		Payload:      tx.Payload,
		Amount:       big.Int(tx.Amount),
		Price:        big.Int(tx.Price),
		R:            big.Int(tx.R),
		S:            big.Int(tx.S),
		V:            big.Int(tx.V),
	}
	if to := tx.Recipient; len(to) != 0 {
		//to := common.HexToAddress(to)
		marshaller.Recipient = common.Hex2Bytes(to)
	}
	return rlp.EncodeToBytes(&marshaller)
}

func validateTx(rlpData hexutil.Bytes, signer types.Signer, isHomestead bool, isIstanbul bool) (*common.Address, *common.Hash, error) {
	tx := new(types.Transaction)
	if err := rlp.DecodeBytes(rlpData, tx); err != nil {
		return nil, nil, err
	}
	sender, err := types.Sender(signer, tx)
	if err != nil {
		return nil, nil, err
	}
	// Intrinsic gas
	requiredGas, err := core.IntrinsicGas(tx.Data(), tx.To() == nil, isHomestead, isIstanbul)
	if err != nil {
		return nil, nil, err
	}
	if requiredGas > tx.Gas() {
		return nil, nil, fmt.Errorf("insufficient gas ( %d < %d )", tx.Gas(), requiredGas)
	}
	h := tx.Hash()
	return &sender, &h, nil
}

type expResult struct {
	sender *common.Address
	valid  bool
}

func buildExpectations(expectations []expectSection) map[string]*expResult {
	forkOrder := []string{"Frontier", "Homestead", "EIP150", "EIP158", "Byzantium", "Constantinople", "Istanbul"}

	allAfter := func(fork string) []string {
		for i, f := range forkOrder {
			if f == fork {
				return forkOrder[i:]
			}
		}
		return nil
	}

	var forks []string
	var mapping = make(map[string]*expResult)
	for _, section := range expectations {
		for _, fork := range section.Network {
			if strings.HasPrefix(fork, ">=") {
				fork = strings.TrimPrefix(fork, ">=")
				forks = allAfter(fork)
			} else {
				forks = []string{fork}
			}
			r := &expResult{valid: section.Result == "valid"}
			if len(section.Sender) > 0 {
				a := common.HexToAddress(section.Sender)
				r.sender = &a
			}
			for _, fork := range forks {
				mapping[fork] = r
			}
		}
	}
	return mapping
}

func fill(filler txFiller, chainID *big.Int) (map[string]interface{}, error) {
	rlpData, err := rlpTx(filler.Transaction)

	if err != nil {
		return nil, err
	}
	expectMap := buildExpectations(filler.Expect)

	verifyExpectations := func(testcaseName string, sender *common.Address) error {

		if exp, ok := expectMap[testcaseName]; ok {
			if exp.valid {
				// It should be a valid transaction
				// Not all tests have a sender as part of the expectation.
				// Only verify that if present
				if exp.sender != nil {
					if *exp.sender != *sender {
						return fmt.Errorf("got %x, expected %x", sender, exp.sender)
					}
				} else if sender == nil {
					return fmt.Errorf("got invalid tx, expected valid")
				}
			} else {
				// sender should be nil, test was an invalid tx
				if sender != nil {
					return fmt.Errorf("got %x on invalid tx", sender)
				}
			}

		} else {
			return fmt.Errorf("missing fork %v", testcaseName)
		}
		return nil
	}

	type ttFork struct {
		Sender *common.UnprefixedAddress `json:"sender"`
		Hash   *common.UnprefixedHash    `json:"hash"`
	}

	testToFill := make(map[string]interface{})
	testToFill["rlp"] = hexutil.Bytes(rlpData)
	for _, testcase := range []struct {
		name        string
		signer      types.Signer
		isHomestead bool
		isIstanbul  bool
	}{
		{"Frontier", types.FrontierSigner{}, false, false},
		{"Homestead", types.HomesteadSigner{}, true, false},
		{"EIP150", types.HomesteadSigner{}, true, false},
		{"EIP158", types.NewEIP155Signer(chainID), true, false},
		{"Byzantium", types.NewEIP155Signer(chainID), true, false},
		{"Constantinople", types.NewEIP155Signer(chainID), true, false},
		{"Istanbul", types.NewEIP155Signer(chainID), true, true},
	} {
		sender, hash, txerr := validateTx(rlpData, testcase.signer, testcase.isHomestead, testcase.isIstanbul)
		err := verifyExpectations(testcase.name, sender)
		if err != nil {
			fmt.Printf("Expectation failed:\n %v\n %v\n txerr: %v\n", testcase.name, err, txerr)
			return nil, err
		}

		if sender != nil {
			s := common.UnprefixedAddress(*sender)
			h := common.UnprefixedHash(*hash)
			testToFill[testcase.name] = ttFork{Sender: &s, Hash: &h}
		} else {
			testToFill[testcase.name] = make(map[string]string)
		}
	}
	return testToFill, nil
}

func fillFile(rootPath, path string, info os.FileInfo, err error) error {
	fmt.Printf("walking %v\n", path)
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	var filler = make(map[string]txFiller)
	err = json.Unmarshal(data, &filler)
	if err != nil {
		return err
	}
	path = filepath.Clean(path)
	rootPath = filepath.Clean(rootPath)
	relPath := strings.TrimPrefix(path, rootPath)
	for _, v := range filler {
		filledTest, err := fill(v, big.NewInt(1))
		if err != nil {
			return err
		}
		// Also add the meta-section
		meta := make(map[string]string)
		meta["comment"] = "filled by geth"
		meta["filledwith"] = fmt.Sprintf("go-ethereum/txfill: %v", params.VersionWithCommit(gitCommit, gitDate))
		meta["llcversion"] = "N/A"
		meta["source"] = relPath
		filledTest["_info"] = meta

		data, err := json.MarshalIndent(filledTest, "", " ")
		if err != nil {
			return err
		}
		// Write out the filled one
		if strings.HasSuffix(info.Name(), "Filler.json") {

			fName := strings.TrimSuffix(info.Name(), "Filler.json")
			outdir := filepath.Dir(filepath.Join("generated", relPath))
			os.MkdirAll(outdir, 0755)
			outfile := filepath.Join(outdir, fmt.Sprintf("%v.json", fName))
			if err := ioutil.WriteFile(outfile, data, 0755); err != nil {
				return err
			}
			fmt.Printf("Wrote %v\n", outfile)
		}
	}
	return nil
}
func main() {
	testRepo := "/home/user/workspace/tests"
	walkBase := filepath.Join(testRepo, "src", "TransactionTestsFiller")
	/**
	There are a few known inconsistensies between geth and aleth, which are outlined below
	- geth enforces nonce and gaslimit to fit within uint64,
	- geth allows unlimited value and gasPrice, aleth does not
	*/
	var blacklist = make(map[string]string)
	blacklist["String10MbDataFiller.json"] = "very large test"
	blacklist["TransactionWithGasLimitxPriceOverflow_correctSFiller.json"] = "geth only accepts uint64 gaslimit"
	blacklist["TransactionWithHighNonce256_correctSFiller.json"] = "geth only accepts uint64 nonce, test expects to pass"
	blacklist["TransactionWithGasLimitxPriceOverflowFiller.json"] = "geth rejects gaslimit > 64 bits in rlp"
	blacklist["TransactionWithGasPriceOverflowFiller.json"] = "geth accepts arbitrary large gasPrice, test expects it not to"
	blacklist["TransactionWithHighNonce256Filler.json"] = "geth only accepts uint64 nonce, test expects to pass"
	blacklist["TransactionWithTooManyRLPElementsFiller.json"] = "no support for rlp stuffing of elements"
	blacklist["TransactionWithHighValueOverflowFiller.json"] = "geth accepts arbitrary large tx value, test expects it not to"

	dirinfo, err := os.Stat(walkBase)
	if os.IsNotExist(err) || !dirinfo.IsDir() {
		fmt.Printf("can't find test files in %s", walkBase)
		os.Exit(1)
	}

	err = filepath.Walk(walkBase, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}
		if reason, exist := blacklist[filepath.Base(path)]; !exist {
			fmt.Printf("Skipping %v (%v)\n", path, reason)
			return nil
		}
		// We also blacklist all 'Copier' tests, which are basically already filled, and should just be
		// copied
		if strings.HasSuffix(path, "Copier.json") {
			reason := "Copier tests are not implemented"
			fmt.Printf("Skipping %v (%v)\n", path, reason)
			return nil
		}

		return fillFile(walkBase, path, info, err)
	})
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
