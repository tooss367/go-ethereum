// Copyright 2021 The go-ethereum Authors
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

package snap

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/holiman/uint256"
)

// hashRange is a utility to handle ranges of hashes, Split up the
// hash-space into sections, and 'walk' over the sections
type hashRange struct {
	current  *uint256.Int
	stepSize *uint256.Int
}

// newHashRange creates a new hashRange, initiated at the start position,
// and with the step set to fill the desired 'num' chunks
func newHashRange(start common.Hash, num uint64) *hashRange {
	i := uint256.NewInt()
	i.SetBytes32(start[:])

	// split the remaining range in 'num' sections
	max := uint256.NewInt().SetAllOne()
	remaining := max.Sub(max, i)
	remaining.Div(remaining, uint256.NewInt().SetUint64(num))

	return &hashRange{current: i, stepSize: remaining}
}

// Next increments the current position by 1 and returns the hash
func (r *hashRange) Next() common.Hash {
	r.current.Add(r.current, uint256.NewInt().SetOne())
	return common.Hash(r.current.Bytes32())
}

// Step increments the current position by 'step' and returns the hash
func (r *hashRange) Step() common.Hash {
	r.current.Add(r.current, r.stepSize)
	return common.Hash(r.current.Bytes32())
}

// incHash returns the next hash, in lexicographical order (a.k.a plus one)
func incHash(h common.Hash) common.Hash {
	a := uint256.NewInt().SetBytes32(h[:])
	a.Add(a, uint256.NewInt().SetOne())
	return common.Hash(a.Bytes32())
}
