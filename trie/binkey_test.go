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
