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
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

// TODO (@holiman): fix up this testcase properly
func TestHashRange(t *testing.T) {
	lastKey := common.HexToHash("0x1000000000000000000000000000000000000000000000000000000000000000")
	nKeys := 10000
	estimate, err := estimateRemainingSlots(nKeys, lastKey)
	if err != nil {
		t.Fatal(err)
	}
	chunks := 1 + estimate/(2*10000)
	t.Logf("Chunks: %d", chunks)

	r := newHashRange(lastKey, chunks)
	t.Logf("Step size: %x", r.step)
	next := common.Hash{}
	last := r.End()
	i := 0
	fmt.Printf("Chunk %d, from %x to %x\n", i, next, last)
	chunks--
	i++
	for ; chunks > 0; chunks, i = chunks-1, i+1 {
		r.Next()
		next = r.Start()
		last = r.End()
		fmt.Printf("Chunk %d, from %x to %x\n", i, next, last)
	}
}
