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

package vm

import (
	"errors"

	"github.com/ethereum/go-ethereum/params"
)

type (
	executionFunc      func(pc *uint64, interpreter *EVMInterpreter, contract *Contract, memory *Memory, stack *Stack) ([]byte, error)
	stackExecutionFunc func(stack *Stack, pool *intPool)
	blockExecutionFunc func(stack *Stack, blockInfo *BlockContext, pool *intPool)
	noErrorFunc        func(pc *uint64, interpreter *EVMInterpreter, contract *Contract, memory *Memory, stack *Stack)
	gasFunc            func(params.GasTable, *EVM, *Contract, *Stack, *Memory, uint64) (uint64, error) // last parameter is the requested memory size as a uint64
	// memorySizeFunc returns the required size, and whether the operation overflowed a uint64
	memorySizeFunc func(*Stack) (size uint64, overflow bool)
)

var errGasUintOverflow = errors.New("gas uint64 overflow")

type operation struct {
	execute     executionFunc      // execute is the ful operation function
	stackExec   stackExecutionFunc // stackExec is for ops that only require a stack
	blockExec   blockExecutionFunc // for ops that require only block constants (and stack)
	noErrorExec noErrorFunc
	constantGas uint64
	dynamicGas  gasFunc
	// minStack tells how many stack items are required
	minStack int
	// maxStack specifies the max length the stack can have for this operation
	// to not overflow the stack.
	maxStack int

	// memorySize returns the memory size required for the operation
	memorySize memorySizeFunc

	halts   bool // indicates whether the operation should halt further execution
	jumps   bool // indicates whether the program counter should not increment
	writes  bool // determines whether this a state modifying operation
	valid   bool // indication whether the retrieved operation is valid and known
	reverts bool // determines whether the operation reverts state (implicitly halts)
	returns bool // determines whether the operations sets the return data content
}

var (
	frontierInstructionSet       = newFrontierInstructionSet()
	homesteadInstructionSet      = newHomesteadInstructionSet()
	byzantiumInstructionSet      = newByzantiumInstructionSet()
	constantinopleInstructionSet = newConstantinopleInstructionSet()
)

// NewConstantinopleInstructionSet returns the frontier, homestead
// byzantium and contantinople instructions.
func newConstantinopleInstructionSet() [256]operation {
	// instructions that can be executed during the byzantium phase.
	instructionSet := newByzantiumInstructionSet()
	instructionSet[SHL] = operation{
		stackExec:   opSHL,
		constantGas: GasFastestStep,
		minStack:    minStack(2, 1),
		maxStack:    maxStack(2, 1),
		valid:       true,
	}
	instructionSet[SHR] = operation{
		stackExec:   opSHR,
		constantGas: GasFastestStep,
		minStack:    minStack(2, 1),
		maxStack:    maxStack(2, 1),
		valid:       true,
	}
	instructionSet[SAR] = operation{
		stackExec:   opSAR,
		constantGas: GasFastestStep,
		minStack:    minStack(2, 1),
		maxStack:    maxStack(2, 1),
		valid:       true,
	}
	instructionSet[EXTCODEHASH] = operation{
		noErrorExec: opExtCodeHash,
		dynamicGas:  gasExtCodeHash,
		minStack:    minStack(1, 1),
		maxStack:    maxStack(1, 1),
		valid:       true,
	}
	instructionSet[CREATE2] = operation{
		execute:    opCreate2,
		dynamicGas: gasCreate2,
		minStack:   minStack(4, 1),
		maxStack:   maxStack(4, 1),
		memorySize: memoryCreate2,
		valid:      true,
		writes:     true,
		returns:    true,
	}
	return instructionSet
}

// NewByzantiumInstructionSet returns the frontier, homestead and
// byzantium instructions.
func newByzantiumInstructionSet() [256]operation {
	// instructions that can be executed during the homestead phase.
	instructionSet := newHomesteadInstructionSet()
	instructionSet[STATICCALL] = operation{
		execute:    opStaticCall,
		dynamicGas: gasStaticCall,
		minStack:   minStack(6, 1),
		maxStack:   maxStack(6, 1),
		memorySize: memoryStaticCall,
		valid:      true,
		returns:    true,
	}
	instructionSet[RETURNDATASIZE] = operation{
		noErrorExec: opReturnDataSize,
		constantGas: GasQuickStep,
		minStack:    minStack(0, 1),
		maxStack:    maxStack(0, 1),
		valid:       true,
	}
	instructionSet[RETURNDATACOPY] = operation{
		execute:    opReturnDataCopy,
		dynamicGas: gasReturnDataCopy,
		minStack:   minStack(3, 0),
		maxStack:   maxStack(3, 0),
		memorySize: memoryReturnDataCopy,
		valid:      true,
	}
	instructionSet[REVERT] = operation{
		execute:    opRevert,
		dynamicGas: gasRevert,
		minStack:   minStack(2, 0),
		maxStack:   maxStack(2, 0),
		memorySize: memoryRevert,
		valid:      true,
		reverts:    true,
		returns:    true,
	}
	return instructionSet
}

// NewHomesteadInstructionSet returns the frontier and homestead
// instructions that can be executed during the homestead phase.
func newHomesteadInstructionSet() [256]operation {
	instructionSet := newFrontierInstructionSet()
	instructionSet[DELEGATECALL] = operation{
		execute:    opDelegateCall,
		dynamicGas: gasDelegateCall,
		minStack:   minStack(6, 1),
		maxStack:   maxStack(6, 1),
		memorySize: memoryDelegateCall,
		valid:      true,
		returns:    true,
	}
	return instructionSet
}

// NewFrontierInstructionSet returns the frontier instructions
// that can be executed during the frontier phase.
func newFrontierInstructionSet() [256]operation {
	return [256]operation{
		STOP: {
			stackExec:   opStop,
			constantGas: 0,
			minStack:    minStack(0, 0),
			maxStack:    maxStack(0, 0),
			halts:       true,
			valid:       true,
		},
		ADD: {
			stackExec:   opAdd,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		MUL: {
			stackExec:   opMul,
			constantGas: GasFastStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		SUB: {
			stackExec:   opSub,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		DIV: {
			stackExec:   opDiv,
			constantGas: GasFastStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		SDIV: {
			stackExec:   opSdiv,
			constantGas: GasFastStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		MOD: {
			stackExec:   opMod,
			constantGas: GasFastStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		SMOD: {
			stackExec:   opSmod,
			constantGas: GasFastStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		ADDMOD: {
			stackExec:   opAddmod,
			constantGas: GasMidStep,
			minStack:    minStack(3, 1),
			maxStack:    maxStack(3, 1),
			valid:       true,
		},
		MULMOD: {
			stackExec:   opMulmod,
			constantGas: GasMidStep,
			minStack:    minStack(3, 1),
			maxStack:    maxStack(3, 1),
			valid:       true,
		},
		EXP: {
			stackExec:  opExp,
			dynamicGas: gasExp,
			minStack:   minStack(2, 1),
			maxStack:   maxStack(2, 1),
			valid:      true,
		},
		SIGNEXTEND: {
			stackExec:   opSignExtend,
			constantGas: GasFastStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		LT: {
			stackExec:   opLt,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		GT: {
			stackExec:   opGt,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		SLT: {
			stackExec:   opSlt,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		SGT: {
			stackExec:   opSgt,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		EQ: {
			stackExec:   opEq,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		ISZERO: {
			stackExec:   opIszero,
			constantGas: GasFastestStep,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		AND: {
			stackExec:   opAnd,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		XOR: {
			stackExec:   opXor,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		OR: {
			stackExec:   opOr,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		NOT: {
			stackExec:   opNot,
			constantGas: GasFastestStep,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		BYTE: {
			stackExec:   opByte,
			constantGas: GasFastestStep,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			valid:       true,
		},
		SHA3: {
			noErrorExec: opSha3,
			dynamicGas:  gasSha3,
			minStack:    minStack(2, 1),
			maxStack:    maxStack(2, 1),
			memorySize:  memorySha3,
			valid:       true,
		},
		ADDRESS: {
			noErrorExec: opAddress,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		BALANCE: {
			noErrorExec: opBalance,
			dynamicGas:  gasBalance,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		ORIGIN: {
			noErrorExec: opOrigin,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		CALLER: {
			noErrorExec: opCaller,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		CALLVALUE: {
			noErrorExec: opCallValue,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		CALLDATALOAD: {
			noErrorExec: opCallDataLoad,
			constantGas: GasFastestStep,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		CALLDATASIZE: {
			noErrorExec: opCallDataSize,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		CALLDATACOPY: {
			noErrorExec: opCallDataCopy,
			dynamicGas:  gasCallDataCopy,
			minStack:    minStack(3, 0),
			maxStack:    maxStack(3, 0),
			memorySize:  memoryCallDataCopy,
			valid:       true,
		},
		CODESIZE: {
			noErrorExec: opCodeSize,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		CODECOPY: {
			noErrorExec: opCodeCopy,
			dynamicGas:  gasCodeCopy,
			minStack:    minStack(3, 0),
			maxStack:    maxStack(3, 0),
			memorySize:  memoryCodeCopy,
			valid:       true,
		},
		GASPRICE: {
			noErrorExec: opGasprice,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		EXTCODESIZE: {
			noErrorExec: opExtCodeSize,
			dynamicGas:  gasExtCodeSize,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		EXTCODECOPY: {
			noErrorExec: opExtCodeCopy,
			dynamicGas:  gasExtCodeCopy,
			minStack:    minStack(4, 0),
			maxStack:    maxStack(4, 0),
			memorySize:  memoryExtCodeCopy,
			valid:       true,
		},
		BLOCKHASH: {
			noErrorExec: opBlockhash,
			constantGas: GasExtStep,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		COINBASE: {
			blockExec:   opCoinbase,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		TIMESTAMP: {
			blockExec:   opTimestamp,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		NUMBER: {
			blockExec:   opNumber,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		DIFFICULTY: {
			blockExec:   opDifficulty,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		GASLIMIT: {
			blockExec:   opGasLimit,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		POP: {
			stackExec:   opPop,
			constantGas: GasQuickStep,
			minStack:    minStack(1, 0),
			maxStack:    maxStack(1, 0),
			valid:       true,
		},
		MLOAD: {
			noErrorExec: opMload,
			dynamicGas:  gasMLoad,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			memorySize:  memoryMLoad,
			valid:       true,
		},
		MSTORE: {
			noErrorExec: opMstore,
			dynamicGas:  gasMStore,
			minStack:    minStack(2, 0),
			maxStack:    maxStack(2, 0),
			memorySize:  memoryMStore,
			valid:       true,
		},
		MSTORE8: {
			noErrorExec: opMstore8,
			dynamicGas:  gasMStore8,
			memorySize:  memoryMStore8,
			minStack:    minStack(2, 0),
			maxStack:    maxStack(2, 0),

			valid: true,
		},
		SLOAD: {
			noErrorExec: opSload,
			dynamicGas:  gasSLoad,
			minStack:    minStack(1, 1),
			maxStack:    maxStack(1, 1),
			valid:       true,
		},
		SSTORE: {
			noErrorExec: opSstore,
			dynamicGas:  gasSStore,
			minStack:    minStack(2, 0),
			maxStack:    maxStack(2, 0),
			valid:       true,
			writes:      true,
		},
		JUMP: {
			execute:     opJump,
			constantGas: GasMidStep,
			minStack:    minStack(1, 0),
			maxStack:    maxStack(1, 0),
			jumps:       true,
			valid:       true,
		},
		JUMPI: {
			execute:     opJumpi,
			constantGas: GasSlowStep,
			minStack:    minStack(2, 0),
			maxStack:    maxStack(2, 0),
			jumps:       true,
			valid:       true,
		},
		PC: {
			noErrorExec: opPc,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		MSIZE: {
			noErrorExec: opMsize,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		GAS: {
			noErrorExec: opGas,
			constantGas: GasQuickStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		JUMPDEST: {
			stackExec:   opJumpdest,
			constantGas: params.JumpdestGas,
			minStack:    minStack(0, 0),
			maxStack:    maxStack(0, 0),
			valid:       true,
		},
		PUSH1: {
			noErrorExec: opPush1,
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH2: {
			noErrorExec: makePush(2, 2),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH3: {
			noErrorExec: makePush(3, 3),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH4: {
			noErrorExec: makePush(4, 4),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH5: {
			noErrorExec: makePush(5, 5),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH6: {
			noErrorExec: makePush(6, 6),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH7: {
			noErrorExec: makePush(7, 7),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH8: {
			noErrorExec: makePush(8, 8),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH9: {
			noErrorExec: makePush(9, 9),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH10: {
			noErrorExec: makePush(10, 10),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH11: {
			noErrorExec: makePush(11, 11),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH12: {
			noErrorExec: makePush(12, 12),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH13: {
			noErrorExec: makePush(13, 13),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH14: {
			noErrorExec: makePush(14, 14),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH15: {
			noErrorExec: makePush(15, 15),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH16: {
			noErrorExec: makePush(16, 16),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH17: {
			noErrorExec: makePush(17, 17),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH18: {
			noErrorExec: makePush(18, 18),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH19: {
			noErrorExec: makePush(19, 19),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH20: {
			noErrorExec: makePush(20, 20),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH21: {
			noErrorExec: makePush(21, 21),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH22: {
			noErrorExec: makePush(22, 22),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH23: {
			noErrorExec: makePush(23, 23),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH24: {
			noErrorExec: makePush(24, 24),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH25: {
			noErrorExec: makePush(25, 25),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH26: {
			noErrorExec: makePush(26, 26),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH27: {
			noErrorExec: makePush(27, 27),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH28: {
			noErrorExec: makePush(28, 28),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH29: {
			noErrorExec: makePush(29, 29),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH30: {
			noErrorExec: makePush(30, 30),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH31: {
			noErrorExec: makePush(31, 31),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		PUSH32: {
			noErrorExec: makePush(32, 32),
			constantGas: GasFastestStep,
			minStack:    minStack(0, 1),
			maxStack:    maxStack(0, 1),
			valid:       true,
		},
		DUP1: {
			stackExec:   makeDup(1),
			constantGas: GasFastestStep,
			minStack:    minDupStack(1),
			maxStack:    maxDupStack(1),
			valid:       true,
		},
		DUP2: {
			stackExec:   makeDup(2),
			constantGas: GasFastestStep,
			minStack:    minDupStack(2),
			maxStack:    maxDupStack(2),
			valid:       true,
		},
		DUP3: {
			stackExec:   makeDup(3),
			constantGas: GasFastestStep,
			minStack:    minDupStack(3),
			maxStack:    maxDupStack(3),
			valid:       true,
		},
		DUP4: {
			stackExec:   makeDup(4),
			constantGas: GasFastestStep,
			minStack:    minDupStack(4),
			maxStack:    maxDupStack(4),
			valid:       true,
		},
		DUP5: {
			stackExec:   makeDup(5),
			constantGas: GasFastestStep,
			minStack:    minDupStack(5),
			maxStack:    maxDupStack(5),
			valid:       true,
		},
		DUP6: {
			stackExec:   makeDup(6),
			constantGas: GasFastestStep,
			minStack:    minDupStack(6),
			maxStack:    maxDupStack(6),
			valid:       true,
		},
		DUP7: {
			stackExec:   makeDup(7),
			constantGas: GasFastestStep,
			minStack:    minDupStack(7),
			maxStack:    maxDupStack(7),
			valid:       true,
		},
		DUP8: {
			stackExec:   makeDup(8),
			constantGas: GasFastestStep,
			minStack:    minDupStack(8),
			maxStack:    maxDupStack(8),
			valid:       true,
		},
		DUP9: {
			stackExec:   makeDup(9),
			constantGas: GasFastestStep,
			minStack:    minDupStack(9),
			maxStack:    maxDupStack(9),
			valid:       true,
		},
		DUP10: {
			stackExec:   makeDup(10),
			constantGas: GasFastestStep,
			minStack:    minDupStack(10),
			maxStack:    maxDupStack(10),
			valid:       true,
		},
		DUP11: {
			stackExec:   makeDup(11),
			constantGas: GasFastestStep,
			minStack:    minDupStack(11),
			maxStack:    maxDupStack(11),
			valid:       true,
		},
		DUP12: {
			stackExec:   makeDup(12),
			constantGas: GasFastestStep,
			minStack:    minDupStack(12),
			maxStack:    maxDupStack(12),
			valid:       true,
		},
		DUP13: {
			stackExec:   makeDup(13),
			constantGas: GasFastestStep,
			minStack:    minDupStack(13),
			maxStack:    maxDupStack(13),
			valid:       true,
		},
		DUP14: {
			stackExec:   makeDup(14),
			constantGas: GasFastestStep,
			minStack:    minDupStack(14),
			maxStack:    maxDupStack(14),
			valid:       true,
		},
		DUP15: {
			stackExec:   makeDup(15),
			constantGas: GasFastestStep,
			minStack:    minDupStack(15),
			maxStack:    maxDupStack(15),
			valid:       true,
		},
		DUP16: {
			stackExec:   makeDup(16),
			constantGas: GasFastestStep,
			minStack:    minDupStack(16),
			maxStack:    maxDupStack(16),
			valid:       true,
		},
		SWAP1: {
			stackExec:   makeSwap(1),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(2),
			maxStack:    maxSwapStack(2),
			valid:       true,
		},
		SWAP2: {
			stackExec:   makeSwap(2),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(3),
			maxStack:    maxSwapStack(3),
			valid:       true,
		},
		SWAP3: {
			stackExec:   makeSwap(3),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(4),
			maxStack:    maxSwapStack(4),
			valid:       true,
		},
		SWAP4: {
			stackExec:   makeSwap(4),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(5),
			maxStack:    maxSwapStack(5),
			valid:       true,
		},
		SWAP5: {
			stackExec:   makeSwap(5),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(6),
			maxStack:    maxSwapStack(6),
			valid:       true,
		},
		SWAP6: {
			stackExec:   makeSwap(6),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(7),
			maxStack:    maxSwapStack(7),
			valid:       true,
		},
		SWAP7: {
			stackExec:   makeSwap(7),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(8),
			maxStack:    maxSwapStack(8),
			valid:       true,
		},
		SWAP8: {
			stackExec:   makeSwap(8),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(9),
			maxStack:    maxSwapStack(9),
			valid:       true,
		},
		SWAP9: {
			stackExec:   makeSwap(9),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(10),
			maxStack:    maxSwapStack(10),
			valid:       true,
		},
		SWAP10: {
			stackExec:   makeSwap(10),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(11),
			maxStack:    maxSwapStack(11),
			valid:       true,
		},
		SWAP11: {
			stackExec:   makeSwap(11),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(12),
			maxStack:    maxSwapStack(12),
			valid:       true,
		},
		SWAP12: {
			stackExec:   makeSwap(12),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(13),
			maxStack:    maxSwapStack(13),
			valid:       true,
		},
		SWAP13: {
			stackExec:   makeSwap(13),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(14),
			maxStack:    maxSwapStack(14),
			valid:       true,
		},
		SWAP14: {
			stackExec:   makeSwap(14),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(15),
			maxStack:    maxSwapStack(15),
			valid:       true,
		},
		SWAP15: {
			stackExec:   makeSwap(15),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(16),
			maxStack:    maxSwapStack(16),
			valid:       true,
		},
		SWAP16: {
			stackExec:   makeSwap(16),
			constantGas: GasFastestStep,
			minStack:    minSwapStack(17),
			maxStack:    maxSwapStack(17),
			valid:       true,
		},
		LOG0: {
			noErrorExec: makeLog(0),
			dynamicGas:  makeGasLog(0),
			minStack:    minStack(2, 0),
			maxStack:    maxStack(2, 0),
			memorySize:  memoryLog,
			valid:       true,
			writes:      true,
		},
		LOG1: {
			noErrorExec: makeLog(1),
			dynamicGas:  makeGasLog(1),
			minStack:    minStack(3, 0),
			maxStack:    maxStack(3, 0),
			memorySize:  memoryLog,
			valid:       true,
			writes:      true,
		},
		LOG2: {
			noErrorExec: makeLog(2),
			dynamicGas:  makeGasLog(2),
			minStack:    minStack(4, 0),
			maxStack:    maxStack(4, 0),
			memorySize:  memoryLog,
			valid:       true,
			writes:      true,
		},
		LOG3: {
			noErrorExec: makeLog(3),
			dynamicGas:  makeGasLog(3),
			minStack:    minStack(5, 0),
			maxStack:    maxStack(5, 0),
			memorySize:  memoryLog,
			valid:       true,
			writes:      true,
		},
		LOG4: {
			noErrorExec: makeLog(4),
			dynamicGas:  makeGasLog(4),
			minStack:    minStack(6, 0),
			maxStack:    maxStack(6, 0),
			memorySize:  memoryLog,
			valid:       true,
			writes:      true,
		},
		CREATE: {
			execute:    opCreate,
			dynamicGas: gasCreate,
			minStack:   minStack(3, 1),
			maxStack:   maxStack(3, 1),
			memorySize: memoryCreate,
			valid:      true,
			writes:     true,
			returns:    true,
		},
		CALL: {
			execute:    opCall,
			dynamicGas: gasCall,
			minStack:   minStack(7, 1),
			maxStack:   maxStack(7, 1),
			memorySize: memoryCall,
			valid:      true,
			returns:    true,
		},
		CALLCODE: {
			execute:    opCallCode,
			dynamicGas: gasCallCode,
			minStack:   minStack(7, 1),
			maxStack:   maxStack(7, 1),
			memorySize: memoryCall,
			valid:      true,
			returns:    true,
		},
		RETURN: {
			execute:    opReturn,
			dynamicGas: gasReturn,
			minStack:   minStack(2, 0),
			maxStack:   maxStack(2, 0),
			memorySize: memoryReturn,
			halts:      true,
			valid:      true,
		},
		SELFDESTRUCT: {
			noErrorExec: opSuicide,
			dynamicGas:  gasSuicide,
			minStack:    minStack(1, 0),
			maxStack:    maxStack(1, 0),
			halts:       true,
			valid:       true,
			writes:      true,
		},
	}
}
