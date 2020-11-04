package trie

import (
	"math/bits"
)

var masks = []byte{0x00, 0x01, 0x03, 0x07, 0x0f, 0x1f, 0x3f, 0x7f, 0xff}

type binaryKey struct {
	key    []byte // full bytes denoting the key
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

func newPartialBinaryKey(key []byte, bitLen int) *binaryKey {
	k := make([]byte, len(key))
	copy(k, key)
	return &binaryKey{
		k,
		bitLen,
	}
}

// Copy creates a copy of the binaryKey, from the given offset
func (b *binaryKey) Copy(start int) *binaryKey{
	panic("implement me")
	// Calculate required size,
	// copy bytes over
	// loop and shift each byte,
	// fix up the bitlen
}

// Len returns the number of bits in the key
func (b *binaryKey) Len() int {
	if len(b.key) < 2 {
		// If zero or 1 byte, the bitlen contains the full length of bits
		return b.bitlen
	}
	return 8*len(b.key) + b.bitlen - 8
}

func (b *binaryKey) commonLength(other *binaryKey) int {
	oLen := other.Len()
	bLen := b.Len()

	smaller, larger := b, other

	if oLen < bLen {
		smaller, larger = other, b
	}
	byteLen := len(smaller.key)
	if byteLen > 1 {
		// At least some full-bytes
		for i, x := range smaller.key[:byteLen-2] {
			if x == larger.key[i] {
				continue
			}
			// The byte differs. Now figure out on which bit it differs
			// We can do that by xoring then and counting leading zeroes
			y := larger.key[i] ^ x
			return 8*i + bits.LeadingZeros8(uint8(y))
		}
	}
	// We've checked all full bytes. Now we need to look at the last byte(s)
	x, y := smaller.key[byteLen-1], larger.key[byteLen-1]
	// We're only interested in differences in the N bits that are
	// covered by the bitmask
	// Therefore, we OR the negated bitmask to force 1:s into that location
	mask := ^masks[smaller.bitlen]
	c := (x ^ y) | mask
	return 8*(byteLen-1) + bits.LeadingZeros8(uint8(c))
}

// byteAt returns the byte starting at bit position bitpos. If the binaryKey
// does is smaller than a full byte from that position, it is zero-padded from
// the right.
// Example
// key: { 0, 1, 0}
// bitpos: 1
// byteAt(bitpos:0) -> 0b0100 0000
// byteAt(bitpos:1) -> 0b1000 0000
func (b *binaryKey) byteAt(bitpos int) byte {
	offset := bitpos / 8
	x := b.key[offset]
	shift := bitpos % 8
	if shift > 0 {
		x <<= shift
		if len(b.key) > offset {
			x |= b.key[offset+1] >> (8 - shift)
		}
	}
	return x
}

func (b *binaryKey) uint32At(bitpos int) uint32 {
	offset := bitpos / 8
	x := uint32(b.key[offset+3]) | uint32(b.key[offset+2])<<8 | uint32(b.key[offset+1])<<16 | uint32(b.key[offset])<<24
	shift := bitpos % 8
	if shift > 0 {
		x <<= shift
		if len(b.key) > offset {
			x |= uint32(b.key[offset+4]) >> (8 - shift)
		}
	}
	return x
}

// subMatch returns true iff b contains the bits of other at the given offset
func (k *binaryKey) subMatch(k2 *binaryKey, offset int) bool {
	// Exit early if the other is too large to fit
	bitsRemaining := k2.Len()
	if offset+bitsRemaining > k.Len() {
		return false
	}
	// offset is the bit-offset on the 'k' key
	offsetY := 0 // offsetY is the bit-offset on the 'k2' key
	// chomp 32 bits at a time
	for bitsRemaining > 32 {
		if x, y := k.uint32At(offset), k2.uint32At(offsetY); x != y {
			return false
		}
		offset += 32
		offsetY += 32
		bitsRemaining -= 32
	}
	// chomp 8 bits at a time
	for bitsRemaining > 8 {
		if x, y := k.byteAt(offset), k2.byteAt(offsetY); x != y {
			return false
		}
		offset += 8
		offsetY += 8
		bitsRemaining -= 8
	}
	// and handle straggling bits
	if bitsRemaining > 0 {
		x := k.byteAt(offset)
		y := k2.byteAt(offsetY)
		res := (x ^ y) >> (8 - bitsRemaining)
		if res != 0 {
			return false
		}
	}
	return true
}

func (b *binaryKey) IsSet(bit int) bool{
	if bit > b.Len(){
		return false
	}
	byteOffset, bitOffset := bit/8, bit % 8
	return b.key[byteOffset] & (1 << (7-bitOffset)) != 0
}