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
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto/sha3"
	"github.com/ethereum/go-ethereum/params"
)

var (
	bigZero                  = new(big.Int)
	tt255                    = math.BigPow(2, 255)
	errWriteProtection       = errors.New("evm: write protection")
	errReturnDataOutOfBounds = errors.New("evm: return data out of bounds")
	errExecutionReverted     = errors.New("evm: execution reverted")
	errMaxCodeSizeExceeded   = errors.New("evm: max code size exceeded")
)

func opAdd(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	math.U256(y.Add(x, y))

	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opSub(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	math.U256(y.Sub(x, y))

	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opMul(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.pop()
	ctx.stack.push(math.U256(x.Mul(x, y)))

	ctx.interpreter.intPool.put(y)

	return nil, nil
}

func opDiv(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	if y.Sign() != 0 {
		math.U256(y.Div(x, y))
	} else {
		y.SetUint64(0)
	}
	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opSdiv(ctx *executionContext) ([]byte, error) {
	x, y := math.S256(ctx.stack.pop()), math.S256(ctx.stack.pop())
	res := ctx.interpreter.intPool.getZero()

	if y.Sign() == 0 || x.Sign() == 0 {
		ctx.stack.push(res)
	} else {
		if x.Sign() != y.Sign() {
			res.Div(x.Abs(x), y.Abs(y))
			res.Neg(res)
		} else {
			res.Div(x.Abs(x), y.Abs(y))
		}
		ctx.stack.push(math.U256(res))
	}
	ctx.interpreter.intPool.put(x, y)
	return nil, nil
}

func opMod(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.pop()
	if y.Sign() == 0 {
		ctx.stack.push(x.SetUint64(0))
	} else {
		ctx.stack.push(math.U256(x.Mod(x, y)))
	}
	ctx.interpreter.intPool.put(y)
	return nil, nil
}

func opSmod(ctx *executionContext) ([]byte, error) {
	x, y := math.S256(ctx.stack.pop()), math.S256(ctx.stack.pop())
	res := ctx.interpreter.intPool.getZero()

	if y.Sign() == 0 {
		ctx.stack.push(res)
	} else {
		if x.Sign() < 0 {
			res.Mod(x.Abs(x), y.Abs(y))
			res.Neg(res)
		} else {
			res.Mod(x.Abs(x), y.Abs(y))
		}
		ctx.stack.push(math.U256(res))
	}
	ctx.interpreter.intPool.put(x, y)
	return nil, nil
}

func opExp(ctx *executionContext) ([]byte, error) {
	base, exponent := ctx.stack.pop(), ctx.stack.pop()
	ctx.stack.push(math.Exp(base, exponent))

	ctx.interpreter.intPool.put(base, exponent)

	return nil, nil
}

func opSignExtend(ctx *executionContext) ([]byte, error) {
	back := ctx.stack.pop()
	if back.Cmp(big.NewInt(31)) < 0 {
		bit := uint(back.Uint64()*8 + 7)
		num := ctx.stack.pop()
		mask := back.Lsh(common.Big1, bit)
		mask.Sub(mask, common.Big1)
		if num.Bit(int(bit)) > 0 {
			num.Or(num, mask.Not(mask))
		} else {
			num.And(num, mask)
		}

		ctx.stack.push(math.U256(num))
	}

	ctx.interpreter.intPool.put(back)
	return nil, nil
}

func opNot(ctx *executionContext) ([]byte, error) {
	x := ctx.stack.peek()
	math.U256(x.Not(x))
	return nil, nil
}

func opLt(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	if x.Cmp(y) < 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opGt(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	if x.Cmp(y) > 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opSlt(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()

	xSign := x.Cmp(tt255)
	ySign := y.Cmp(tt255)

	switch {
	case xSign >= 0 && ySign < 0:
		y.SetUint64(1)

	case xSign < 0 && ySign >= 0:
		y.SetUint64(0)

	default:
		if x.Cmp(y) < 0 {
			y.SetUint64(1)
		} else {
			y.SetUint64(0)
		}
	}
	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opSgt(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()

	xSign := x.Cmp(tt255)
	ySign := y.Cmp(tt255)

	switch {
	case xSign >= 0 && ySign < 0:
		y.SetUint64(0)

	case xSign < 0 && ySign >= 0:
		y.SetUint64(1)

	default:
		if x.Cmp(y) > 0 {
			y.SetUint64(1)
		} else {
			y.SetUint64(0)
		}
	}
	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opEq(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	if x.Cmp(y) == 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opIszero(ctx *executionContext) ([]byte, error) {
	x := ctx.stack.peek()
	if x.Sign() > 0 {
		x.SetUint64(0)
	} else {
		x.SetUint64(1)
	}
	return nil, nil
}

func opAnd(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.pop()
	ctx.stack.push(x.And(x, y))

	ctx.interpreter.intPool.put(y)
	return nil, nil
}

func opOr(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	y.Or(x, y)

	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opXor(ctx *executionContext) ([]byte, error) {
	x, y := ctx.stack.pop(), ctx.stack.peek()
	y.Xor(x, y)

	ctx.interpreter.intPool.put(x)
	return nil, nil
}

func opByte(ctx *executionContext) ([]byte, error) {
	th, val := ctx.stack.pop(), ctx.stack.peek()
	if th.Cmp(common.Big32) < 0 {
		b := math.Byte(val, 32, int(th.Int64()))
		val.SetUint64(uint64(b))
	} else {
		val.SetUint64(0)
	}
	ctx.interpreter.intPool.put(th)
	return nil, nil
}

func opAddmod(ctx *executionContext) ([]byte, error) {
	x, y, z := ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop()
	if z.Cmp(bigZero) > 0 {
		x.Add(x, y)
		x.Mod(x, z)
		ctx.stack.push(math.U256(x))
	} else {
		ctx.stack.push(x.SetUint64(0))
	}
	ctx.interpreter.intPool.put(y, z)
	return nil, nil
}

func opMulmod(ctx *executionContext) ([]byte, error) {
	x, y, z := ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop()
	if z.Cmp(bigZero) > 0 {
		x.Mul(x, y)
		x.Mod(x, z)
		ctx.stack.push(math.U256(x))
	} else {
		ctx.stack.push(x.SetUint64(0))
	}
	ctx.interpreter.intPool.put(y, z)
	return nil, nil
}

// opSHL implements Shift Left
// The SHL instruction (shift left) pops 2 values from the stack, first arg1 and then arg2,
// and pushes on the stack arg2 shifted to the left by arg1 number of bits.
func opSHL(ctx *executionContext) ([]byte, error) {
	// Note, second operand is left in the stack; accumulate result into it, and no need to push it afterwards
	shift, value := math.U256(ctx.stack.pop()), math.U256(ctx.stack.peek())
	defer ctx.interpreter.intPool.put(shift) // First operand back into the pool

	if shift.Cmp(common.Big256) >= 0 {
		value.SetUint64(0)
		return nil, nil
	}
	n := uint(shift.Uint64())
	math.U256(value.Lsh(value, n))

	return nil, nil
}

// opSHR implements Logical Shift Right
// The SHR instruction (logical shift right) pops 2 values from the stack, first arg1 and then arg2,
// and pushes on the stack arg2 shifted to the right by arg1 number of bits with zero fill.
func opSHR(ctx *executionContext) ([]byte, error) {
	// Note, second operand is left in the stack; accumulate result into it, and no need to push it afterwards
	shift, value := math.U256(ctx.stack.pop()), math.U256(ctx.stack.peek())
	defer ctx.interpreter.intPool.put(shift) // First operand back into the pool

	if shift.Cmp(common.Big256) >= 0 {
		value.SetUint64(0)
		return nil, nil
	}
	n := uint(shift.Uint64())
	math.U256(value.Rsh(value, n))

	return nil, nil
}

// opSAR implements Arithmetic Shift Right
// The SAR instruction (arithmetic shift right) pops 2 values from the stack, first arg1 and then arg2,
// and pushes on the stack arg2 shifted to the right by arg1 number of bits with sign extension.
func opSAR(ctx *executionContext) ([]byte, error) {
	// Note, S256 returns (potentially) a new bigint, so we're popping, not peeking this one
	shift, value := math.U256(ctx.stack.pop()), math.S256(ctx.stack.pop())
	defer ctx.interpreter.intPool.put(shift) // First operand back into the pool

	if shift.Cmp(common.Big256) >= 0 {
		if value.Sign() >= 0 {
			value.SetUint64(0)
		} else {
			value.SetInt64(-1)
		}
		ctx.stack.push(math.U256(value))
		return nil, nil
	}
	n := uint(shift.Uint64())
	value.Rsh(value, n)
	ctx.stack.push(math.U256(value))

	return nil, nil
}

func opSha3(ctx *executionContext) ([]byte, error) {
	offset, size := ctx.stack.pop(), ctx.stack.pop()
	data := ctx.memory.Get(offset.Int64(), size.Int64())
	evm := ctx.interpreter.evm
	interpreter := ctx.interpreter

	if interpreter.hasher == nil {
		interpreter.hasher = sha3.NewKeccak256().(keccakState)
	} else {
		interpreter.hasher.Reset()
	}
	interpreter.hasher.Write(data)
	interpreter.hasher.Read(interpreter.hasherBuf[:])

	if evm.vmConfig.EnablePreimageRecording {
		evm.StateDB.AddPreimage(interpreter.hasherBuf, data)
	}
	ctx.stack.push(interpreter.intPool.get().SetBytes(interpreter.hasherBuf[:]))

	interpreter.intPool.put(offset, size)
	return nil, nil
}

func opAddress(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.contract.Address().Big())
	return nil, nil
}

func opBalance(ctx *executionContext) ([]byte, error) {
	slot := ctx.stack.peek()
	slot.Set(ctx.interpreter.evm.StateDB.GetBalance(common.BigToAddress(slot)))
	return nil, nil
}

func opOrigin(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.evm.Origin.Big())
	return nil, nil
}

func opCaller(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.contract.Caller().Big())
	return nil, nil
}

func opCallValue(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().Set(ctx.contract.value))
	return nil, nil
}

func opCallDataLoad(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().SetBytes(getDataBig(ctx.contract.Input, ctx.stack.pop(), big32)))
	return nil, nil
}

func opCallDataSize(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().SetInt64(int64(len(ctx.contract.Input))))
	return nil, nil
}

func opCallDataCopy(ctx *executionContext) ([]byte, error) {
	var (
		memOffset  = ctx.stack.pop()
		dataOffset = ctx.stack.pop()
		length     = ctx.stack.pop()
	)
	ctx.memory.Set(memOffset.Uint64(), length.Uint64(), getDataBig(ctx.contract.Input, dataOffset, length))

	ctx.interpreter.intPool.put(memOffset, dataOffset, length)
	return nil, nil
}

func opReturnDataSize(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(uint64(len(ctx.interpreter.returnData))))
	return nil, nil
}

func opReturnDataCopy(ctx *executionContext) ([]byte, error) {
	var (
		memOffset  = ctx.stack.pop()
		dataOffset = ctx.stack.pop()
		length     = ctx.stack.pop()

		end = ctx.interpreter.intPool.get().Add(dataOffset, length)
	)
	defer ctx.interpreter.intPool.put(memOffset, dataOffset, length, end)

	if end.BitLen() > 64 || uint64(len(ctx.interpreter.returnData)) < end.Uint64() {
		return nil, errReturnDataOutOfBounds
	}
	ctx.memory.Set(memOffset.Uint64(), length.Uint64(), ctx.interpreter.returnData[dataOffset.Uint64():end.Uint64()])

	return nil, nil
}

func opExtCodeSize(ctx *executionContext) ([]byte, error) {
	slot := ctx.stack.peek()
	slot.SetUint64(uint64(ctx.interpreter.evm.StateDB.GetCodeSize(common.BigToAddress(slot))))

	return nil, nil
}

func opCodeSize(ctx *executionContext) ([]byte, error) {
	l := ctx.interpreter.intPool.get().SetInt64(int64(len(ctx.contract.Code)))
	ctx.stack.push(l)

	return nil, nil
}

func opCodeCopy(ctx *executionContext) ([]byte, error) {
	var (
		memOffset  = ctx.stack.pop()
		codeOffset = ctx.stack.pop()
		length     = ctx.stack.pop()
	)
	codeCopy := getDataBig(ctx.contract.Code, codeOffset, length)
	ctx.memory.Set(memOffset.Uint64(), length.Uint64(), codeCopy)

	ctx.interpreter.intPool.put(memOffset, codeOffset, length)
	return nil, nil
}

func opExtCodeCopy(ctx *executionContext) ([]byte, error) {
	var (
		addr       = common.BigToAddress(ctx.stack.pop())
		memOffset  = ctx.stack.pop()
		codeOffset = ctx.stack.pop()
		length     = ctx.stack.pop()
	)
	codeCopy := getDataBig(ctx.interpreter.evm.StateDB.GetCode(addr), codeOffset, length)
	ctx.memory.Set(memOffset.Uint64(), length.Uint64(), codeCopy)

	ctx.interpreter.intPool.put(memOffset, codeOffset, length)
	return nil, nil
}

// opExtCodeHash returns the code hash of a specified account.
// There are several cases when the function is called, while we can relay everything
// to `state.GetCodeHash` function to ensure the correctness.
//   (1) Caller tries to get the code hash of a normal contract account, state
// should return the relative code hash and set it as the result.
//
//   (2) Caller tries to get the code hash of a non-existent account, state should
// return common.Hash{} and zero will be set as the result.
//
//   (3) Caller tries to get the code hash for an account without contract code,
// state should return emptyCodeHash(0xc5d246...) as the result.
//
//   (4) Caller tries to get the code hash of a precompiled account, the result
// should be zero or emptyCodeHash.
//
// It is worth noting that in order to avoid unnecessary create and clean,
// all precompile accounts on mainnet have been transferred 1 wei, so the return
// here should be emptyCodeHash.
// If the precompile account is not transferred any amount on a private or
// customized chain, the return value will be zero.
//
//   (5) Caller tries to get the code hash for an account which is marked as suicided
// in the current transaction, the code hash of this account should be returned.
//
//   (6) Caller tries to get the code hash for an account which is marked as deleted,
// this account should be regarded as a non-existent account and zero should be returned.
func opExtCodeHash(ctx *executionContext) ([]byte, error) {
	slot := ctx.stack.peek()
	slot.SetBytes(ctx.interpreter.evm.StateDB.GetCodeHash(common.BigToAddress(slot)).Bytes())
	return nil, nil
}

func opGasprice(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().Set(ctx.interpreter.evm.GasPrice))
	return nil, nil
}

func opBlockhash(ctx *executionContext) ([]byte, error) {
	num := ctx.stack.pop()

	n := ctx.interpreter.intPool.get().Sub(ctx.interpreter.evm.BlockNumber, common.Big257)
	if num.Cmp(n) > 0 && num.Cmp(ctx.interpreter.evm.BlockNumber) < 0 {
		ctx.stack.push(ctx.interpreter.evm.GetHash(num.Uint64()).Big())
	} else {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	}
	ctx.interpreter.intPool.put(num, n)
	return nil, nil
}

func opCoinbase(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.evm.Coinbase.Big())
	return nil, nil
}

func opTimestamp(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(math.U256(ctx.interpreter.intPool.get().Set(ctx.interpreter.evm.Time)))
	return nil, nil
}

func opNumber(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(math.U256(ctx.interpreter.intPool.get().Set(ctx.interpreter.evm.BlockNumber)))
	return nil, nil
}

func opDifficulty(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(math.U256(ctx.interpreter.intPool.get().Set(ctx.interpreter.evm.Difficulty)))
	return nil, nil
}

func opGasLimit(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(math.U256(ctx.interpreter.intPool.get().SetUint64(ctx.interpreter.evm.GasLimit)))
	return nil, nil
}

func opPop(ctx *executionContext) ([]byte, error) {
	ctx.interpreter.intPool.put(ctx.stack.pop())
	return nil, nil
}

func opMload(ctx *executionContext) ([]byte, error) {
	offset := ctx.stack.pop()
	val := ctx.interpreter.intPool.get().SetBytes(ctx.memory.Get(offset.Int64(), 32))
	ctx.stack.push(val)

	ctx.interpreter.intPool.put(offset)
	return nil, nil
}

func opMstore(ctx *executionContext) ([]byte, error) {
	// pop value of the stack
	mStart, val := ctx.stack.pop(), ctx.stack.pop()
	ctx.memory.Set32(mStart.Uint64(), val)

	ctx.interpreter.intPool.put(mStart, val)
	return nil, nil
}

func opMstore8(ctx *executionContext) ([]byte, error) {
	off, val := ctx.stack.pop().Int64(), ctx.stack.pop().Int64()
	ctx.memory.store[off] = byte(val & 0xff)

	return nil, nil
}

func opSload(ctx *executionContext) ([]byte, error) {
	loc := ctx.stack.peek()
	val := ctx.interpreter.evm.StateDB.GetState(ctx.contract.Address(), common.BigToHash(loc))
	loc.SetBytes(val.Bytes())
	return nil, nil
}

func opSstore(ctx *executionContext) ([]byte, error) {
	loc := common.BigToHash(ctx.stack.pop())
	val := ctx.stack.pop()
	ctx.interpreter.evm.StateDB.SetState(ctx.contract.Address(), loc, common.BigToHash(val))

	ctx.interpreter.intPool.put(val)
	return nil, nil
}

func opJump(ctx *executionContext) ([]byte, error) {
	pos := ctx.stack.pop()
	if !ctx.contract.validJumpdest(pos) {
		nop := ctx.contract.GetOp(pos.Uint64())
		return nil, fmt.Errorf("invalid jump destination (%v) %v", nop, pos)
	}
	*ctx.pc = pos.Uint64()

	ctx.interpreter.intPool.put(pos)
	return nil, nil
}

func opJumpi(ctx *executionContext) ([]byte, error) {
	pos, cond := ctx.stack.pop(), ctx.stack.pop()
	if cond.Sign() != 0 {
		if !ctx.contract.validJumpdest(pos) {
			nop := ctx.contract.GetOp(pos.Uint64())
			return nil, fmt.Errorf("invalid jump destination (%v) %v", nop, pos)
		}
		*ctx.pc = pos.Uint64()
	} else {
		*ctx.pc++
	}

	ctx.interpreter.intPool.put(pos, cond)
	return nil, nil
}

func opJumpdest(ctx *executionContext) ([]byte, error) {
	return nil, nil
}

func opPc(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(*ctx.pc))
	return nil, nil
}

func opMsize(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().SetInt64(int64(ctx.memory.Len())))
	return nil, nil
}

func opGas(ctx *executionContext) ([]byte, error) {
	ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(ctx.contract.Gas))
	return nil, nil
}

func opCreate(ctx *executionContext) ([]byte, error) {
	var (
		value        = ctx.stack.pop()
		offset, size = ctx.stack.pop(), ctx.stack.pop()
		input        = ctx.memory.Get(offset.Int64(), size.Int64())
		gas          = ctx.contract.Gas
	)
	if ctx.interpreter.evm.ChainConfig().IsEIP150(ctx.interpreter.evm.BlockNumber) {
		gas -= gas / 64
	}

	ctx.contract.UseGas(gas)
	res, addr, returnGas, suberr := ctx.interpreter.evm.Create(ctx.contract, input, gas, value)
	// Push item on the stack based on the returned error. If the ruleset is
	// homestead we must check for CodeStoreOutOfGasError (homestead only
	// rule) and treat as an error, if the ruleset is frontier we must
	// ignore this error and pretend the operation was successful.
	if ctx.interpreter.evm.ChainConfig().IsHomestead(ctx.interpreter.evm.BlockNumber) && suberr == ErrCodeStoreOutOfGas {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else if suberr != nil && suberr != ErrCodeStoreOutOfGas {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else {
		ctx.stack.push(addr.Big())
	}
	ctx.contract.Gas += returnGas
	ctx.interpreter.intPool.put(value, offset, size)

	if suberr == errExecutionReverted {
		return res, nil
	}
	return nil, nil
}

func opCreate2(ctx *executionContext) ([]byte, error) {
	var (
		endowment    = ctx.stack.pop()
		offset, size = ctx.stack.pop(), ctx.stack.pop()
		salt         = ctx.stack.pop()
		input        = ctx.memory.Get(offset.Int64(), size.Int64())
		gas          = ctx.contract.Gas
	)

	// Apply EIP150
	gas -= gas / 64
	ctx.contract.UseGas(gas)
	res, addr, returnGas, suberr := ctx.interpreter.evm.Create2(ctx.contract, input, gas, endowment, salt)
	// Push item on the stack based on the returned error.
	if suberr != nil {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else {
		ctx.stack.push(addr.Big())
	}
	ctx.contract.Gas += returnGas
	ctx.interpreter.intPool.put(endowment, offset, size, salt)

	if suberr == errExecutionReverted {
		return res, nil
	}
	return nil, nil
}

func opCall(ctx *executionContext) ([]byte, error) {
	// Pop gas. The actual gas in ctx.interpreter.evm.callGasTemp.
	ctx.interpreter.intPool.put(ctx.stack.pop())
	gas := ctx.interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, value, inOffset, inSize, retOffset, retSize := ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop()
	toAddr := common.BigToAddress(addr)
	value = math.U256(value)
	// Get the arguments from the ctx.memory
	args := ctx.memory.Get(inOffset.Int64(), inSize.Int64())

	if value.Sign() != 0 {
		gas += params.CallStipend
	}
	ret, returnGas, err := ctx.interpreter.evm.Call(ctx.contract, toAddr, args, gas, value)
	if err != nil {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else {
		ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(1))
	}
	if err == nil || err == errExecutionReverted {
		ctx.memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}
	ctx.contract.Gas += returnGas

	ctx.interpreter.intPool.put(addr, value, inOffset, inSize, retOffset, retSize)
	return ret, nil
}

func opCallCode(ctx *executionContext) ([]byte, error) {
	// Pop gas. The actual gas is in ctx.interpreter.evm.callGasTemp.
	ctx.interpreter.intPool.put(ctx.stack.pop())
	gas := ctx.interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, value, inOffset, inSize, retOffset, retSize := ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop()
	toAddr := common.BigToAddress(addr)
	value = math.U256(value)
	// Get arguments from the memory.
	args := ctx.memory.Get(inOffset.Int64(), inSize.Int64())

	if value.Sign() != 0 {
		gas += params.CallStipend
	}
	ret, returnGas, err := ctx.interpreter.evm.CallCode(ctx.contract, toAddr, args, gas, value)
	if err != nil {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else {
		ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(1))
	}
	if err == nil || err == errExecutionReverted {
		ctx.memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}
	ctx.contract.Gas += returnGas

	ctx.interpreter.intPool.put(addr, value, inOffset, inSize, retOffset, retSize)
	return ret, nil
}

func opDelegateCall(ctx *executionContext) ([]byte, error) {
	// Pop gas. The actual gas is in ctx.interpreter.evm.callGasTemp.
	ctx.interpreter.intPool.put(ctx.stack.pop())
	gas := ctx.interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, inOffset, inSize, retOffset, retSize := ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop()
	toAddr := common.BigToAddress(addr)
	// Get arguments from the ctx.memory
	args := ctx.memory.Get(inOffset.Int64(), inSize.Int64())

	ret, returnGas, err := ctx.interpreter.evm.DelegateCall(ctx.contract, toAddr, args, gas)
	if err != nil {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else {
		ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(1))
	}
	if err == nil || err == errExecutionReverted {
		ctx.memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}
	ctx.contract.Gas += returnGas

	ctx.interpreter.intPool.put(addr, inOffset, inSize, retOffset, retSize)
	return ret, nil
}

func opStaticCall(ctx *executionContext) ([]byte, error) {
	// Pop gas. The actual gas is in ctx.interpreter.evm.callGasTemp.
	ctx.interpreter.intPool.put(ctx.stack.pop())
	gas := ctx.interpreter.evm.callGasTemp
	// Pop other call parameters.
	addr, inOffset, inSize, retOffset, retSize := ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop(), ctx.stack.pop()
	toAddr := common.BigToAddress(addr)
	// Get arguments from the ctx.memory
	args := ctx.memory.Get(inOffset.Int64(), inSize.Int64())

	ret, returnGas, err := ctx.interpreter.evm.StaticCall(ctx.contract, toAddr, args, gas)
	if err != nil {
		ctx.stack.push(ctx.interpreter.intPool.getZero())
	} else {
		ctx.stack.push(ctx.interpreter.intPool.get().SetUint64(1))
	}
	if err == nil || err == errExecutionReverted {
		ctx.memory.Set(retOffset.Uint64(), retSize.Uint64(), ret)
	}
	ctx.contract.Gas += returnGas

	ctx.interpreter.intPool.put(addr, inOffset, inSize, retOffset, retSize)
	return ret, nil
}

func opReturn(ctx *executionContext) ([]byte, error) {
	offset, size := ctx.stack.pop(), ctx.stack.pop()
	ret := ctx.memory.GetPtr(offset.Int64(), size.Int64())

	ctx.interpreter.intPool.put(offset, size)
	return ret, nil
}

func opRevert(ctx *executionContext) ([]byte, error) {
	offset, size := ctx.stack.pop(), ctx.stack.pop()
	ret := ctx.memory.GetPtr(offset.Int64(), size.Int64())

	ctx.interpreter.intPool.put(offset, size)
	return ret, nil
}

func opStop(ctx *executionContext) ([]byte, error) {
	return nil, nil
}

func opSuicide(ctx *executionContext) ([]byte, error) {
	balance := ctx.interpreter.evm.StateDB.GetBalance(ctx.contract.Address())
	ctx.interpreter.evm.StateDB.AddBalance(common.BigToAddress(ctx.stack.pop()), balance)

	ctx.interpreter.evm.StateDB.Suicide(ctx.contract.Address())
	return nil, nil
}

// following functions are used by the instruction jump  table

// make log instruction function
func makeLog(size int) executionFunc {
	return func(ctx *executionContext) ([]byte, error) {
		topics := make([]common.Hash, size)
		mStart, mSize := ctx.stack.pop(), ctx.stack.pop()
		for i := 0; i < size; i++ {
			topics[i] = common.BigToHash(ctx.stack.pop())
		}

		d := ctx.memory.Get(mStart.Int64(), mSize.Int64())
		ctx.interpreter.evm.StateDB.AddLog(&types.Log{
			Address: ctx.contract.Address(),
			Topics:  topics,
			Data:    d,
			// This is a non-consensus field, but assigned here because
			// core/state doesn't know the current block number.
			BlockNumber: ctx.interpreter.evm.BlockNumber.Uint64(),
		})

		ctx.interpreter.intPool.put(mStart, mSize)
		return nil, nil
	}
}

// make push instruction function
func makePush(size uint64, pushByteSize int) executionFunc {
	return func(ctx *executionContext) ([]byte, error) {
		codeLen := len(ctx.contract.Code)

		startMin := codeLen
		if int(*ctx.pc+1) < startMin {
			startMin = int(*ctx.pc + 1)
		}

		endMin := codeLen
		if startMin+pushByteSize < endMin {
			endMin = startMin + pushByteSize
		}

		integer := ctx.interpreter.intPool.get()
		ctx.stack.push(integer.SetBytes(common.RightPadBytes(ctx.contract.Code[startMin:endMin], pushByteSize)))

		*ctx.pc += size
		return nil, nil
	}
}

// make dup instruction function
func makeDup(size int64) executionFunc {
	return func(ctx *executionContext) ([]byte, error) {
		ctx.stack.dup(ctx.interpreter.intPool, int(size))
		return nil, nil
	}
}

// make swap instruction function
func makeSwap(size int64) executionFunc {
	// switch n + 1 otherwise n would be swapped with n
	size++
	return func(ctx *executionContext) ([]byte, error) {
		ctx.stack.swap(int(size))
		return nil, nil
	}
}
