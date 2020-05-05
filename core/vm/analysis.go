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

package vm

import "math/big"

// bitvec is a bit vector which maps bytes in a program.
// An unset bit means the byte is an opcode, a set bit means
// it's data (i.e. argument of PUSHxx).
type bitvec []byte

func (bits *bitvec) set(pos uint64) {
	(*bits)[pos/8] |= 0x80 >> (pos % 8)
}
func (bits *bitvec) set8(pos uint64) {
	(*bits)[pos/8] |= 0xFF >> (pos % 8)
	(*bits)[pos/8+1] |= ^(0xFF >> (pos % 8))
}

// codeSegment checks if the position is in a code segment.
func (bits *bitvec) codeSegment(pos uint64) bool {
	return ((*bits)[pos/8] & (0x80 >> (pos % 8))) == 0
}

// codeSegment checks if the position is in a code segment.
func (bits *bitvec) isSet(pos uint64) bool {
	return ((*bits)[pos/8] & (0x80 >> (pos % 8))) == 1
}

// codeBitmap collects data locations in code.
func codeBitmap(code []byte) bitvec {
	// The bitmap is 4 bytes longer than necessary, in case the code
	// ends with a PUSH32, the algorithm will push zeroes onto the
	// bitvector outside the bounds of the actual code.
	codeDataBitmap := make(bitvec, len(code)/8+1+4)
	for pc := uint64(0); pc < uint64(len(code)); {
		op := OpCode(code[pc])

		if op >= PUSH1 && op <= PUSH32 {
			numbits := op - PUSH1 + 1
			pc++
			for ; numbits >= 8; numbits -= 8 {
				codeDataBitmap.set8(pc) // 8
				pc += 8
			}
			for ; numbits > 0; numbits-- {
				codeDataBitmap.set(pc)
				pc++
			}
		} else {
			pc++
		}
	}
	return codeDataBitmap
}
// codeBitmap collects data locations in code.
func codeBitmap2(code []byte) (bitvec, bitvec) {
	// The bitmap is 4 bytes longer than necessary, in case the code
	// ends with a PUSH32, the algorithm will push zeroes onto the
	// bitvector outside the bounds of the actual code.
	codeDataBitmap := make(bitvec, len(code)/8+1+4)

	// The extra padding of up to 32 bytes is handled by padding with 1 element
	subroutineBitmap := make(bitvec, len(code)/32 + 1)
	for pc := uint64(0); pc < uint64(len(code)); {
		op := OpCode(code[pc])

		if op >= PUSH1 && op <= PUSH32 {
			numbits := op - PUSH1 + 1
			pc++
			for ; numbits >= 8; numbits -= 8 {
				codeDataBitmap.set8(pc) // 8
				pc += 8
			}
			for ; numbits > 0; numbits-- {
				codeDataBitmap.set(pc)
				pc++
			}
		} else {
			if pc % 32 == 0 && op == BEGINSUB  {
				subroutineBitmap.set(pc/32)
			}
			pc++
		}
	}
	return codeDataBitmap, subroutineBitmap
}

// codeBitmap collects data locations in code.
func codeBitmap3(code []byte) (bitvec, []uint16) {
	// The bitmap is 4 bytes longer than necessary, in case the code
	// ends with a PUSH32, the algorithm will push zeroes onto the
	// bitvector outside the bounds of the actual code.
	codeDataBitmap := make(bitvec, len(code)/8+1+4)

	// The extra padding of up to 32 bytes is handled by padding with 1 element
	srSizeMap := make([]uint16, len(code)/32+1)
	curStart := uint64(0)
	for pc := uint64(0); pc < uint64(len(code)); {
		op := OpCode(code[pc])

		if op >= PUSH1 && op <= PUSH32 {
			numbits := op - PUSH1 + 1
			pc++
			for ; numbits >= 8; numbits -= 8 {
				codeDataBitmap.set8(pc) // 8
				pc += 8
			}
			for ; numbits > 0; numbits-- {
				codeDataBitmap.set(pc)
				pc++
			}
		} else {
			if pc % 32 == 0 && op == BEGINSUB  {
				srSizeMap[curStart/32] = uint16((pc - curStart)/32)
				curStart = pc
			}
			pc++
		}
	}
	// Also need to set the final size
	srSizeMap[curStart/32] = uint16((len(code) - curStart)/32)
	return codeDataBitmap, srSizeMap
}



func (analysis *bitvec) validJumpdest(dest *big.Int) bool {
	// PC cannot go beyond len(code) and certainly can't be bigger than 63 bits.
	// Don't bother checking for JUMPDEST in that case.
	if dest.BitLen() >= 63 {
		return false
	}
	udest := dest.Uint64()
	return analysis.codeSegment(udest)
}

func (analysis *bitvec) validJumpdestWithSeek(subroutineBitmap *bitvec, dest *big.Int) bool {
	// PC cannot go beyond len(code) and certainly can't be bigger than 63 bits.
	// Don't bother checking for JUMPDEST in that case.
	if dest.BitLen() >= 63 {
		return false
	}
	udest := dest.Uint64()
	if !analysis.codeSegment(udest){
		return false
	}
	start := uint64(0)

	for i := start; i < udest ; i+=32{
		if subroutineBitmap.isSet(i/32){
			return false
		}
	}
	return true
}

