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

package types

import (
	"bytes"
	"io"

	//"fmt"
	"math"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
)

func TestDecodeEmptyTypedReceipt(t *testing.T) {
	input := []byte{0x80}
	var r Receipt
	err := rlp.DecodeBytes(input, &r)
	if err != errEmptyTypedReceipt {
		t.Fatal("wrong error:", err)
	}
}

// Tests that receipt data can be correctly derived from the contextual infos
func TestDeriveFields(t *testing.T) {
	// Create a few transactions to have receipts for
	to2 := common.HexToAddress("0x2")
	to3 := common.HexToAddress("0x3")
	txs := Transactions{
		NewTx(&LegacyTx{
			Nonce:    1,
			Value:    big.NewInt(1),
			Gas:      1,
			GasPrice: big.NewInt(1),
		}),
		NewTx(&LegacyTx{
			To:       &to2,
			Nonce:    2,
			Value:    big.NewInt(2),
			Gas:      2,
			GasPrice: big.NewInt(2),
		}),
		NewTx(&AccessListTx{
			To:       &to3,
			Nonce:    3,
			Value:    big.NewInt(3),
			Gas:      3,
			GasPrice: big.NewInt(3),
		}),
	}
	// Create the corresponding receipts
	receipts := Receipts{
		&Receipt{
			Status:            ReceiptStatusFailed,
			CumulativeGasUsed: 1,
			Logs: []*Log{
				{Address: common.BytesToAddress([]byte{0x11})},
				{Address: common.BytesToAddress([]byte{0x01, 0x11})},
			},
			TxHash:          txs[0].Hash(),
			ContractAddress: common.BytesToAddress([]byte{0x01, 0x11, 0x11}),
			GasUsed:         1,
		},
		&Receipt{
			PostState:         common.Hash{2}.Bytes(),
			CumulativeGasUsed: 3,
			Logs: []*Log{
				{Address: common.BytesToAddress([]byte{0x22})},
				{Address: common.BytesToAddress([]byte{0x02, 0x22})},
			},
			TxHash:          txs[1].Hash(),
			ContractAddress: common.BytesToAddress([]byte{0x02, 0x22, 0x22}),
			GasUsed:         2,
		},
		&Receipt{
			Type:              AccessListTxType,
			PostState:         common.Hash{3}.Bytes(),
			CumulativeGasUsed: 6,
			Logs: []*Log{
				{Address: common.BytesToAddress([]byte{0x33})},
				{Address: common.BytesToAddress([]byte{0x03, 0x33})},
			},
			TxHash:          txs[2].Hash(),
			ContractAddress: common.BytesToAddress([]byte{0x03, 0x33, 0x33}),
			GasUsed:         3,
		},
	}
	// Clear all the computed fields and re-derive them
	number := big.NewInt(1)
	hash := common.BytesToHash([]byte{0x03, 0x14})

	clearComputedFieldsOnReceipts(t, receipts)
	if err := receipts.DeriveFields(params.TestChainConfig, hash, number.Uint64(), txs); err != nil {
		t.Fatalf("DeriveFields(...) = %v, want <nil>", err)
	}
	// Iterate over all the computed fields and check that they're correct
	signer := MakeSigner(params.TestChainConfig, number)

	logIndex := uint(0)
	for i := range receipts {
		if receipts[i].Type != txs[i].Type() {
			t.Errorf("receipts[%d].Type = %d, want %d", i, receipts[i].Type, txs[i].Type())
		}
		if receipts[i].TxHash != txs[i].Hash() {
			t.Errorf("receipts[%d].TxHash = %s, want %s", i, receipts[i].TxHash.String(), txs[i].Hash().String())
		}
		if receipts[i].BlockHash != hash {
			t.Errorf("receipts[%d].BlockHash = %s, want %s", i, receipts[i].BlockHash.String(), hash.String())
		}
		if receipts[i].BlockNumber.Cmp(number) != 0 {
			t.Errorf("receipts[%c].BlockNumber = %s, want %s", i, receipts[i].BlockNumber.String(), number.String())
		}
		if receipts[i].TransactionIndex != uint(i) {
			t.Errorf("receipts[%d].TransactionIndex = %d, want %d", i, receipts[i].TransactionIndex, i)
		}
		if receipts[i].GasUsed != txs[i].Gas() {
			t.Errorf("receipts[%d].GasUsed = %d, want %d", i, receipts[i].GasUsed, txs[i].Gas())
		}
		if txs[i].To() != nil && receipts[i].ContractAddress != (common.Address{}) {
			t.Errorf("receipts[%d].ContractAddress = %s, want %s", i, receipts[i].ContractAddress.String(), (common.Address{}).String())
		}
		from, _ := Sender(signer, txs[i])
		contractAddress := crypto.CreateAddress(from, txs[i].Nonce())
		if txs[i].To() == nil && receipts[i].ContractAddress != contractAddress {
			t.Errorf("receipts[%d].ContractAddress = %s, want %s", i, receipts[i].ContractAddress.String(), contractAddress.String())
		}
		for j := range receipts[i].Logs {
			if receipts[i].Logs[j].BlockNumber != number.Uint64() {
				t.Errorf("receipts[%d].Logs[%d].BlockNumber = %d, want %d", i, j, receipts[i].Logs[j].BlockNumber, number.Uint64())
			}
			if receipts[i].Logs[j].BlockHash != hash {
				t.Errorf("receipts[%d].Logs[%d].BlockHash = %s, want %s", i, j, receipts[i].Logs[j].BlockHash.String(), hash.String())
			}
			if receipts[i].Logs[j].TxHash != txs[i].Hash() {
				t.Errorf("receipts[%d].Logs[%d].TxHash = %s, want %s", i, j, receipts[i].Logs[j].TxHash.String(), txs[i].Hash().String())
			}
			if receipts[i].Logs[j].TxHash != txs[i].Hash() {
				t.Errorf("receipts[%d].Logs[%d].TxHash = %s, want %s", i, j, receipts[i].Logs[j].TxHash.String(), txs[i].Hash().String())
			}
			if receipts[i].Logs[j].TxIndex != uint(i) {
				t.Errorf("receipts[%d].Logs[%d].TransactionIndex = %d, want %d", i, j, receipts[i].Logs[j].TxIndex, i)
			}
			if receipts[i].Logs[j].Index != logIndex {
				t.Errorf("receipts[%d].Logs[%d].Index = %d, want %d", i, j, receipts[i].Logs[j].Index, logIndex)
			}
			logIndex++
		}
	}
}

// TestTypedReceiptEncodingDecoding reproduces a flaw that existed in the receipt
// rlp decoder, which failed due to a shadowing error.
func TestTypedReceiptEncodingDecoding(t *testing.T) {
	var payload = common.FromHex("f9043eb9010c01f90108018262d4b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c0b9010c01f901080182cd14b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c0b9010d01f901090183013754b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c0b9010d01f90109018301a194b9010000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c0")
	check := func(bundle []*Receipt) {
		t.Helper()
		for i, receipt := range bundle {
			if got, want := receipt.Type, uint8(1); got != want {
				t.Fatalf("bundle %d: got %x, want %x", i, got, want)
			}
		}
	}
	{
		var bundle []*Receipt
		rlp.DecodeBytes(payload, &bundle)
		check(bundle)
	}
	{
		var bundle []*Receipt
		r := bytes.NewReader(payload)
		s := rlp.NewStream(r, uint64(len(payload)))
		if err := s.Decode(&bundle); err != nil {
			t.Fatal(err)
		}
		check(bundle)
	}
}

func clearComputedFieldsOnReceipts(t *testing.T, receipts Receipts) {
	t.Helper()

	for _, receipt := range receipts {
		clearComputedFieldsOnReceipt(t, receipt)
	}
}

func clearComputedFieldsOnReceipt(t *testing.T, receipt *Receipt) {
	t.Helper()

	receipt.TxHash = common.Hash{}
	receipt.BlockHash = common.Hash{}
	receipt.BlockNumber = big.NewInt(math.MaxUint32)
	receipt.TransactionIndex = math.MaxUint32
	receipt.ContractAddress = common.Address{}
	receipt.GasUsed = 0

	clearComputedFieldsOnLogs(t, receipt.Logs)
}

func clearComputedFieldsOnLogs(t *testing.T, logs []*Log) {
	t.Helper()

	for _, log := range logs {
		clearComputedFieldsOnLog(t, log)
	}
}

func clearComputedFieldsOnLog(t *testing.T, log *Log) {
	t.Helper()

	log.BlockNumber = math.MaxUint32
	log.BlockHash = common.Hash{}
	log.TxHash = common.Hash{}
	log.TxIndex = math.MaxUint32
	log.Index = math.MaxUint32
}

type devnull struct{ len int }

func (d *devnull) Write(p []byte) (n int, err error) {
	d.len += len(p)
	return len(p), nil
}

func BenchmarkReceiptMarshall(b *testing.B) {

	log := &Log{
		Address: common.Address{},
		Topics:  []common.Hash{common.Hash{}},
		Data:    []byte("data"),
	}
	r := &Receipt{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 0x888888888,
		Logs:              []*Log{log, log, log, log, log},
	}
	b.Run("receipt", func(b *testing.B) {
		var null = &devnull{}
		for i := 0; i < b.N; i++ {
			rlp.Encode(null, r)
		}
		b.SetBytes(int64(null.len / b.N))
	})
	b.Run("receipt-storage", func(b *testing.B) {
		var null = &devnull{}
		for i := 0; i < b.N; i++ {
			rlp.Encode(null, (*ReceiptForStorage)(r))
		}
		b.SetBytes(int64(null.len / b.N))
	})
	b.Run("receipt-custom", func(b *testing.B) {
		var null = &devnull{}
		for i := 0; i < b.N; i++ {
			encodeReceipt(null, r)
		}
		b.SetBytes(int64(null.len / b.N))
	})

}
func TestReceiptMarshall(t *testing.T) {
	r := &ReceiptForStorage{
		Status:            ReceiptStatusSuccessful,
		CumulativeGasUsed: 0x888888888,
		Logs:              make([]*Log, 5),
	}
	r.Logs[0] = &Log{
		Address: common.Address{},
		Topics:  []common.Hash{common.Hash{}},
		Data:    nil,
	}
	r.Logs[1] = &Log{
		Address: common.Address{},
		Topics:  []common.Hash{common.Hash{}},
		Data:    []byte{0, 1, 2, 3, 4},
	}
	actualRlp, _ := rlp.EncodeToBytes(r)
	t.Logf("act rlp: %x\n", actualRlp)
	t.Logf("rlp size: %d\n", len(actualRlp))

	{

		alen := rlp.IntSize(r.CumulativeGasUsed)
		blen := rlp.IntSize(r.Status)
		var logLen uint64
		for _, l := range r.Logs {
			if l == nil {
				logLen += rlp.ListSize(0)
				continue
			}
			addressLen := 21 //rlp.IntSize(20) + 20 //l.Address
			//fmt.Printf("addressLen est: %d\n", addressLen)
			//x, _ := rlp.EncodeToBytes(l.Address)
			//fmt.Printf("addressLen act: %d\n", len(x))

			dataLen := rlp.ByteSize(l.Data) // l.Data
			//fmt.Printf("dataLen est: %d\n", dataLen)
			//x, _ = rlp.EncodeToBytes(l.Data)
			//fmt.Printf("dataLen act: %d\n", len(x))
			topicsLen := rlp.ListSize(uint64(len(l.Topics) * 33)) //(rlp.IntSize(32) + 32)))
			//fmt.Printf("topicsLen est: %d\n", topicsLen)
			//x, _ := rlp.EncodeToBytes(l.Topics)
			//fmt.Printf("topicsLen act: %d\n", len(x))
			logLen += rlp.ListSize(uint64(addressLen+dataLen) + topicsLen)
			//fmt.Printf("logLen est: %d\n", logLen)
			//x, _ = rlp.EncodeToBytes(l)
			//fmt.Printf("logLen act: %d\n", len(x))

		}
		logTotalSize := rlp.ListSize(uint64(logLen))
		contentLen := uint64(alen+blen) + logTotalSize
		clen := rlp.ListSize(contentLen)

		t.Logf("est size: %d tot %x contentLen %x\n", clen, clen, contentLen)
		a := make([]byte, 0, clen)
		a = rlp.AppendListHeader(a, int(contentLen)) // outer size

		a = rlp.AppendUint64(a, r.Status)
		a = rlp.AppendUint64(a, r.CumulativeGasUsed)

		{ // Now we enter the logs.
			// Size of the logs struct
			a = rlp.AppendListHeader(a, int(logLen)) // outer size
			for _, l := range r.Logs {
				if l == nil {
					a = append(a, 0xc0)
					continue
				}
				addressLen := 21
				dataLen := rlp.ByteSize(l.Data)
				topicsLen := rlp.ListSize(uint64(len(l.Topics) * 33))
				//total := rlp.ListSize(uint64(addressLen+dataLen) + topicsLen)
				a = rlp.AppendListHeader(a, addressLen+dataLen+int(topicsLen))
				// Address
				a = append(a, 0x80+20)
				a = append(a, l.Address[:]...)
				// Topics, a list of hashes
				a = rlp.AppendListHeader(a, len(l.Topics)*33)
				for _, topic := range l.Topics {
					a = append(a, 0x80+32)
					a = append(a, topic.Bytes()...)
				}
				// Data
				a = rlp.AppendBytes(a, l.Data)
			}

		}

		// next is a list  header
		//rlp.AppendUint64(a, )
		//rlp.AppendUint64(a,20) // size of address
		t.Logf("act rlp: %x\n", actualRlp)
		t.Logf("est rlp: %x\n", a)
		// test3
		tt := bytes.NewBuffer(nil)
		encodeReceipt(tt, (*Receipt)(r))
		t.Logf("est rlp: %x\n", tt.Bytes())
	}

}

func encodeReceipt(w io.Writer, r *Receipt) {
	var logLen uint64
	for _, l := range r.Logs {
		if l == nil {
			logLen += 1
			continue
		}
		topicsLen := rlp.ListSize(uint64(len(l.Topics) * 33)) //(rlp.IntSize(32) + 32)))
		logLen += rlp.ListSize(uint64(21+rlp.ByteSize(l.Data)) + topicsLen)
	}
	contentLen := uint64(rlp.IntSize(r.CumulativeGasUsed)+rlp.IntSize(r.Status)) +
		rlp.ListSize(uint64(logLen))
	a := make([]byte, 0, rlp.ListSize(contentLen))
	a = rlp.AppendListHeader(a, int(contentLen))
	a = rlp.AppendUint64(a, r.Status)
	a = rlp.AppendUint64(a, r.CumulativeGasUsed)
	{ // Now we enter the logs.
		a = rlp.AppendListHeader(a, int(logLen)) // outer size of log struct
		for _, l := range r.Logs {
			if l == nil {
				a = append(a, 0xc0)
				continue
			}
			topicsLen := rlp.ListSize(uint64(len(l.Topics) * 33))
			a = rlp.AppendListHeader(a, 21+rlp.ByteSize(l.Data)+int(topicsLen))
			// Address
			a = append(a, 0x80+20, )
			a = append(a, l.Address[:]...)
			// Topics, a list of hashes
			a = rlp.AppendListHeader(a, len(l.Topics)*33)
			for _, topic := range l.Topics {
				a = append(a, 0x80+32)
				a = append(a, topic.Bytes()...)
			}
			// Data
			a = rlp.AppendBytes(a, l.Data)
		}
	}
	w.Write(a)
}
