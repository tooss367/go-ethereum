// Copyright 2019 The go-ethereum Authors
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

package rlp

import (
	"bytes"
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

func decodeEncode(input []byte, val interface{}, i int) {
	if err := rlp.DecodeBytes(input, val); err == nil {
		output, err := rlp.EncodeToBytes(val)
		if err != nil {
			panic(err)
		}
		if !bytes.Equal(input, output) {
			panic(fmt.Sprintf("case %d: encode-decode is not equal, \ninput : %x\noutput: %x", i, input, output))
		}
	}
}

func decodeTx(data []byte) (*types.Transaction, error) {
	var tx types.Transaction
	t, err := &tx, rlp.Decode(bytes.NewReader(data), &tx)

	return t, err
}
func Decode(payload io.Reader, size int, val interface{}) error {
	s := rlp.NewStream(payload, uint64(size))
	if err := s.Decode(val); err != nil {
		return err
	}
	return nil
}

var signers = []types.Signer{
	types.NewEIP155Signer(big.NewInt(1)),
	types.NewEIP155Signer(big.NewInt(0)),
	types.FrontierSigner{},
	types.HomesteadSigner{types.FrontierSigner{}},
}

func Fuzz(input []byte) int {
	payload := bytes.NewReader(input)

	var txs []*types.Transaction

	if err := Decode(payload, len(input), &txs); err != nil {
		return 0
	}

	for _, tx := range txs {
		if tx == nil {
			return 0
		}
	}
	for _, tx := range txs {
		for _, signer := range signers {
			signer.Sender(tx)
		}
	}
	return 0
}
