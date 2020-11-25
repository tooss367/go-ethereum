package main

import "github.com/ethereum/go-ethereum/eth/protocols/eth"

func main() {
	//eth.FuzzA([]byte{0,1,1,2,3,4,5,6,7,9,0})
	eth.FuzzA([]byte("\x00\x10\x00\x00\x18rej\x00rel\x00rif\x00r"))
}
