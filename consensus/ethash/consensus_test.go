// Copyright 2017 The go-ethereum Authors
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

package ethash

import (
	"encoding/json"
	"github.com/ethereum/go-ethereum/common"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

type diffTest struct {
	ParentTimestamp    uint64
	ParentDifficulty   *big.Int
	CurrentTimestamp   uint64
	CurrentBlocknumber *big.Int
	CurrentDifficulty  *big.Int
}

func (d *diffTest) UnmarshalJSON(b []byte) (err error) {
	var ext struct {
		ParentTimestamp    string
		ParentDifficulty   string
		CurrentTimestamp   string
		CurrentBlocknumber string
		CurrentDifficulty  string
	}
	if err := json.Unmarshal(b, &ext); err != nil {
		return err
	}

	d.ParentTimestamp = math.MustParseUint64(ext.ParentTimestamp)
	d.ParentDifficulty = math.MustParseBig256(ext.ParentDifficulty)
	d.CurrentTimestamp = math.MustParseUint64(ext.CurrentTimestamp)
	d.CurrentBlocknumber = math.MustParseBig256(ext.CurrentBlocknumber)
	d.CurrentDifficulty = math.MustParseBig256(ext.CurrentDifficulty)

	return nil
}

func TestCalcDifficulty(t *testing.T) {
	file, err := os.Open(filepath.Join("..", "..", "tests", "testdata", "BasicTests", "difficulty.json"))
	if err != nil {
		t.Skip(err)
	}
	defer file.Close()

	tests := make(map[string]diffTest)
	err = json.NewDecoder(file).Decode(&tests)
	if err != nil {
		t.Fatal(err)
	}

	config := &params.ChainConfig{HomesteadBlock: big.NewInt(1150000)}

	for name, test := range tests {
		number := new(big.Int).Sub(test.CurrentBlocknumber, big.NewInt(1))
		diff := CalcDifficulty(config, test.CurrentTimestamp, &types.Header{
			Number:     number,
			Time:       test.ParentTimestamp,
			Difficulty: test.ParentDifficulty,
		})
		if diff.Cmp(test.CurrentDifficulty) != 0 {
			t.Error(name, "failed. Expected", test.CurrentDifficulty, "and calculated", diff)
		}
	}
}

func TestDifficultyCalculator(t *testing.T) {
	x := makeDifficultyCalculator(big.NewInt(1000000))
	y := makeDifficultyCalculatorU256(big.NewInt(1000000))
	rand.Seed(2)
	for i := 0; i < 50000; i++ {
		// 1 to 300 seconds diff
		var timeDelta = uint64(1 + rand.Uint32()%300)
		var difficulty = make([]byte, 10)
		rand.Read(difficulty)
		h := &types.Header{
			UncleHash:  types.EmptyUncleHash,
			Difficulty: new(big.Int).SetBytes(difficulty),
			Number:     new(big.Int).SetUint64(rand.Uint64() % 50_000_000),
			Time:       rand.Uint64() - timeDelta,
		}
		exp := x(h.Time+timeDelta, h)
		got := y(h.Time+timeDelta, h)
		if exp.BitLen() > 256 {
			continue
		}
		if exp.Cmp(got) != 0 {
			t.Fatalf("test %d: error, got \n%x\n, exp \n%x\nHeader: \n%v\n", i, got, exp, h)
		}
	}
}

func TestDifficultyCalculatorFrontier(t *testing.T) {
	x := calcDifficultyFrontier
	y := calcDifficultyFrontierU256
	rand.Seed(2)
	for i := 0; i < 500000; i++ {
		// 1 to 300 seconds diff
		var timeDelta = uint64(1 + rand.Uint32()%300)
		var difficulty = make([]byte, 10)
		rand.Read(difficulty)
		h := &types.Header{
			UncleHash:  types.EmptyUncleHash,
			Difficulty: new(big.Int).SetBytes(difficulty),
			Number:     new(big.Int).SetUint64(rand.Uint64() % 50_000_000),
			Time:       rand.Uint64() - timeDelta,
		}
		exp := x(h.Time+timeDelta, h)
		got := y(h.Time+timeDelta, h)
		if exp.BitLen() > 256 {
			continue
		}
		if exp.Cmp(got) != 0 {
			t.Fatalf("test %d: error, got \n%x\n, exp \n%x\nHeader: \n%v\n", i, got, exp, h)
		}
	}
}

func BenchmarkDifficultyCalculator(b *testing.B) {
	x1 := makeDifficultyCalculator(big.NewInt(1000000))
	x2 := makeDifficultyCalculatorU256(big.NewInt(1000000))
	x3 := calcDifficultyFrontier
	x4 := calcDifficultyFrontierU256
	h := &types.Header{
		ParentHash: common.Hash{},
		UncleHash:  types.EmptyUncleHash,
		Difficulty: big.NewInt(0xffffff),
		Number:     big.NewInt(500000),
		Time:       1000000,
	}
	b.Run("big", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			x1(1000014, h)
		}
	})
	b.Run("u256", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			x2(1000014, h)
		}
	})
	b.Run("big-frontier", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			x3(1000014, h)
		}
	})
	b.Run("u256-frontier", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			x4(1000014, h)
		}
	})
}
