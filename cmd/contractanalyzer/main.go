package main

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/asm"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"os"
	"strings"
)

type obj struct {
	Code     string `json:"code"`
	CodeHash string `json:"codeHash"`
}

// Check that the code has jump across beginsub
func filter_a(contract *obj) (bool, error) {
	code := common.FromHex(contract.Code)
	if len(code) == 0 {
		return false, fmt.Errorf("no code: %v", contract.Code)
	}
	it := asm.NewInstructionIterator(code)
	jumps, jumpdest, beginsub := false, false, false
	for it.Next() {
		switch it.Op() {
		case vm.JUMP, vm.JUMPI:
			jumps = true
		case vm.JUMPDEST:
			jumpdest = true
		case vm.BEGINSUB:
			beginsub = true
		}
	}
	if jumps && jumpdest && beginsub {
		return true, nil
	}
	fmt.Fprintf(os.Stderr, "discarding, jumps:%v, jumpdest: %v, beginsub: %v  - hash %v\n",
		jumps, jumpdest, beginsub, contract.CodeHash)
	return false, nil
}

// Filters the following pattern:
// JUMP ... BEGINSUB ... JUMPDEST
func filter_b(contract *obj) (bool, error) {
	code := common.FromHex(contract.Code)
	if len(code) == 0 {
		return false, fmt.Errorf("no code: %v", contract.Code)
	}
	it := asm.NewInstructionIterator(code)
	for it.Next() {
		if it.Op() == vm.JUMP || it.Op() == vm.JUMPI {
			break
		}
	}
	for it.Next() {
		if it.Op() == vm.BEGINSUB {
			break
		}
	}
	for it.Next() {
		if it.Op() == vm.JUMPDEST {
			return true, nil
		}
	}
	fmt.Fprintf(os.Stderr, "discarding, not jump..beginsub..jumpdest  - hash %v\n",
		contract.CodeHash)
	return false, nil
}

// Filters contracts that fail within the first couple of steps
func filter_c(contract *obj) (bool, error) {
	code := common.FromHex(contract.Code)
	if len(code) == 0 {
		return false, fmt.Errorf("no code: %v", contract.Code)
	}
	it := asm.NewInstructionIterator(code)
	instr := vm.AllInstructions
	stackDepth := 0
	var trace []string
	for it.Next() {
		op := instr[it.Op()]
		trace = append(trace, it.Op().String())
		valid, minStack, maxStack := op.Info()
		if !valid {
			t := strings.Join(trace, ",")
			fmt.Fprintf(os.Stderr, "discarding, invalid opcode \n%v\n hash: %v\n",
				t, contract.CodeHash)
			return false, nil
		}
		if it.Op() == vm.STOP {
			t := strings.Join(trace, ",")
			fmt.Fprintf(os.Stderr, "discarding, exec stop \n%v\n hash: %v\n",
				t, contract.CodeHash)
			return false, nil
		}
		if minStack > stackDepth {
			t := strings.Join(trace, ",")
			fmt.Fprintf(os.Stderr, "discarding, shallow stack \n%v\n hash: %v\n",
				t, contract.CodeHash)
			return false, nil
		}
		nPush := int(params.StackLimit) + minStack - maxStack
		stackDepth += nPush
		// here we lose sight of the control flow
		if it.Op() == vm.JUMP || it.Op() == vm.JUMPI {
			break
		}
	}
	return true, nil
}

// Filter files which
// - have solidity metadata, and the 'offending' portion is found within the metadata only
func filter_d(contract *obj) (bool, error) {
	code := common.FromHex(contract.Code)

	metaSize := int(binary.BigEndian.Uint16(code[len(code)-2:]))
	if metaSize >= len(code)-2 {
		// not metadata
		return true, nil
	}
	realCode := code[:len(code)-2-metaSize]
	res, err := filter_a(&obj{
		Code:     hex.EncodeToString(realCode),
		CodeHash: "",
	})
	if err != nil {
		return false, err
	}
	if !res {
		fmt.Fprintf(os.Stderr, "discarding, 'bad' code was in metadata [1] \n hash: %v\n",
			contract.CodeHash)
	}
	res, err = filter_b(&obj{
		Code:     hex.EncodeToString(realCode),
		CodeHash: "",
	})
	if err != nil {
		return false, err
	}
	if !res {
		fmt.Fprintf(os.Stderr, "discarding, 'bad' code was in metadata [2] \n hash: %v\n",
			contract.CodeHash)
	}
	return res, nil
}

func filter_e(contract *obj) (bool, error) {

	code := common.FromHex(contract.Code)
	it := asm.NewInstructionIterator(code)

	i := 0
	for it.Next() {
		i++
		fmt.Fprintf(os.Stderr, "%v ", it.Op())
		if i > 80 {
			break
		}
	}
	fmt.Println("")
	fmt.Println("")
	return true, nil
}

func manage(p string) error {
	file, err := os.Open(p)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var o = &obj{}
		data := scanner.Bytes()
		if len(data) == 0 {
			continue
		}
		if err := json.Unmarshal(data, o); err != nil {
			return err
		}
		remain, err := filter_a(o)
		if err != nil {
			return err
		}
		if !remain {
			continue
		}
		remain, err = filter_b(o)
		if err != nil {
			return err
		}
		remain, err = filter_c(o)
		if err != nil {
			return err
		}
		if !remain {
			continue
		}
		remain, err = filter_d(o)
		if err != nil {
			return err
		}
		if !remain {
			continue
		}
		//remain, err = filter_e(o)
		//if err != nil {
		//	return err
		//}
		//if !remain {
		//	continue
		//}
		fmt.Fprintln(os.Stdout, string(data))
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "usage: %v <file>", os.Args[0])
		os.Exit(1)
	}
	if err := manage(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
