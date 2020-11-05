package trie

import (
	"github.com/ethereum/go-ethereum/common"
	"testing"
)

func TestCommonLength(t *testing.T) {

	for i, tc := range []struct {
		keyA string
		keyB string
		exp  int
	}{
		{keyA: "0a", keyB: "0a0a", exp: 8},
		{keyA: "01", keyB: "0a0a", exp: 4},
		{keyA: "aabbccddeeff", keyB: "aabbccddeeffaa", exp: 48},
		{keyA: "aabbccddee80", keyB: "aabbccddeeffaa", exp: 41},
	} {
		x := newBinaryKey(common.FromHex(tc.keyA))
		y := newBinaryKey(common.FromHex(tc.keyB))
		if got, exp := x.commonLength(y), tc.exp; got != exp {
			t.Errorf("tc %d error: have %d, want %d", i, got, exp)
		}
		if got, exp := y.commonLength(x), tc.exp; got != exp {
			t.Errorf("tc %d reverse error: have %d, want %d", i, got, exp)
		}
	}
}

func TestSubmatch(t *testing.T) {
	for i, tc := range []struct {
		keyA    string
		bitlenA int
		keyB    string
		bitlenB int
		offset  int
		exp     bool
	}{
		{"aabb", 8, "ab", 8, 4, true},
		{"aabb", 8, "ab", 8, 0, false},
		{"aabbccddeeff", 8, "aabbccddeeff", 8, 0, true},
		{"aabbccddee80", 8, "cc", 8, 16, true},
		{"aabbccddee80", 8, "c0", 4, 16, true},
		{"0011223344556677889900aabbccddeeff", 1, "0022446688aaccef113201557799bbdd", 8, 1, true},
	} {
		x := newPartialBinaryKey(common.FromHex(tc.keyA), tc.bitlenA)
		y := newPartialBinaryKey(common.FromHex(tc.keyB), tc.bitlenB)
		if got, exp := x.subMatch(y, tc.offset), tc.exp; got != exp {
			t.Errorf("tc %d error: have %v, want %v", i, got, exp)
		}
	}
}

func TestBitSet(t *testing.T) {
	for i, tc := range []struct {
		keyA    string
		bitlenA int
		offset  int
		exp     bool
	}{
		{"a1bb", 8, 0, true},
		{"a1bb", 8, 1, false},
		{"a1bb", 8, 2, true},
		{"a1bb", 8, 3, false},
		{"a1bb", 8, 4, false},
		{"a1bb", 8, 5, false},
		{"a1bb", 8, 6, false},
		{"a1bb", 8, 7, true},
		{"a1bb", 1, 8, true},
		{"a1bb", 1, 9, false},
		{"a1bb", 1, 10, false},
		{"a1bb", 1, 11, false},
	} {
		x := newPartialBinaryKey(common.FromHex(tc.keyA), tc.bitlenA)
		if got, exp := x.IsSet(tc.offset), tc.exp; got != exp {
			t.Errorf("tc %d error: have %v, want %v", i, got, exp)
		}
	}
}

func TestCopy(t *testing.T) {
	for i, tc := range []struct {
		keyA    string
		bitlenA int
		offset  int
		exp     string
	}{
		{"a1", 8, 1, "0100001"},
		{"a1bb", 8, 1, "010000110111011"},
		{"a1", 8, 7, "1"},
		{"a1", 8, 8, ""},
		{"a1", 7, 7, ""},
		// TODO more of these
	} {
		x := newPartialBinaryKey(common.FromHex(tc.keyA), tc.bitlenA)
		if got, exp := x.Copy(tc.offset), tc.exp; got.bitString() != exp {
			t.Errorf("tc %d error: have \"%v\", want \"%v\"", i, got.bitString(), exp)
		}

	}
}
