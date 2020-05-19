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

package bls

import "bytes"

type precompile interface{
	RequiredGas(input []byte) uint64
	Run(input []byte) ([]byte, error)
}

func Fuzz(data []byte) int {
	precompiles := []precompile{
		new(bls12381G1Add),// split, swap
		new(bls12381G1Mul),
		new(bls12381G1MultiExp),
		new(bls12381G2Add),
		new(bls12381G2Mul),
		new(bls12381G2MultiExp),
		new(bls12381MapG1),
		new(bls12381MapG2),
		new(bls12381Pairing),
	}
	originaldata := make([]byte, len(data))
	var promote = 0
	copy(originaldata, data)
	for _, precompile := range precompiles{
		precompile.RequiredGas(data)
		_, err := precompile.Run(data)
		if !bytes.Equal(originaldata, data){
			panic("Input modified!")
		}
		if err == nil{ 
			promote |= 1
		}
	}
	return promote
}
