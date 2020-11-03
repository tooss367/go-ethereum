package trie

import (
	"bytes"
	"math/bits"
)

var masks = []byte{0x00, 0x01, 0x03, 0x07, 0x0f, 0x1f, 0x3f, 0x7f, 0xff}

type binaryKey struct {
	k      []byte // full bytes denoting the key
	bitlen int    // number of bits in the last byte
}

func newBinaryKey(key []byte) *binaryKey {
	k := make([]byte, len(key))
	copy(k, key)
	return &binaryKey{
		k,
		8,
	}
}

// Len returns the number of bits in the key
func (b *binaryKey) Len() int{
	if len(b.k) < 2{
		// If zero or 1 byte, the bitlen contains the full length of bits
		return b.bitlen
	}
	return 8*len(b.k) + b.bitlen - 8
}

func (b *binaryKey) commonLength(other *binaryKey) int {
	oLen := other.Len()
	bLen := b.Len()

	smaller, larger := b, other

	if oLen < bLen {
		smaller, larger = other, b
	}
	byteLen := len(smaller.k)
	if byteLen > 1 {
		// At least some full-bytes
		for i, x := range smaller.k[:byteLen-2] {
			if x == larger.k[i] {
				continue
			}
			// The byte differs. Now figure out on which bit it differs
			// We can do that by xoring then and counting leading zeroes
			y := larger.k[i] ^ x
			return 8*i + bits.LeadingZeros8(uint8(y))
		}
	}
	// We've checked all full bytes. Now we need to look at the last byte(s)
	x, y := smaller.k[byteLen-1], larger.k[byteLen-1]
	// We're only interested in differences in the N bits that are
	// covered by the bitmask
	// Therefore, we OR the negated bitmask to force 1:s into that location
	mask := ^masks[smaller.bitlen]
	c := (x ^ y) | mask
	return 8*(byteLen-1) + bits.LeadingZeros8(uint8(c))
}

// subMatch returns true iff b contains the bit of other on the given offset
//func (b *binaryKey) subMatch(other *binaryKey, offset int) bool{
//	How many full-bytes to check?
	//nBytes := offset / 8
	//
	//
	//if !bytes.Equal(b[:nBytes], o[:nBytes])
//
//}