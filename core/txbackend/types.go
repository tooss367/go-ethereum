package txbackend

import (
	"github.com/ethereum/go-ethereum/common"
	//"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
	"math/big"
)

// TxRef contains fields needed for pool scheduling.
// It weighs in on 52 bytes
type TxRef struct {
	sender   common.Address // 20 bytes
	nonce    uint64         // 8 bytes
	gasPrice *big.Int       // 8 bytes
	key      uint64         // 8 bytes
}

func (ref *TxRef) loadTransaction(backend TxBackend) *types.Transaction {
	var tx types.Transaction
	rlpdata := backend.Get(ref.key)
	if err := rlp.DecodeBytes(rlpdata, &tx); err != nil {
		log.Error("Failed looking up transaction", "error", err)
		return nil
	}
	return &tx
}

func (ref *TxRef) Nonce() uint64 {
	return ref.nonce
}

func (ref *TxRef) Sender() common.Address {
	return ref.sender
}

func (ref *TxRef) GasPrice() *big.Int {
	return ref.gasPrice
}

func (ref *TxRef) Gas() uint64 {
	return 0
}

func (tx *TxRef) GasPriceCmp(other *TxRef) int {
	return tx.gasPrice.Cmp(other.gasPrice)
}
func (tx *TxRef) GasPriceIntCmp(other *big.Int) int {
	return tx.gasPrice.Cmp(other)
}
func (tx *TxRef) Hash() common.Hash {
	panic("Not implemented")
}

func (ref *TxRef) Cost() *big.Int {
	// Todo: do we need cost?
	// If so, store it as one big.Int, or keep gasLimit + gasPrice + value as three
	// separate ints?
	return big.NewInt(0)
}

// Size return the size of the RLP-encoded transaction
func (ref *TxRef) Size() uint64 {
	panic("Implement me")
}

func fromTransaction(backend TxBackend, tx *types.Transaction, sender common.Address) *TxRef {
	rlpData, _ := rlp.EncodeToBytes(tx)
	key := backend.Put(rlpData)
	return &TxRef{
		sender:   sender,
		nonce:    tx.Nonce(),
		gasPrice: tx.GasPrice(),
		key:      key,
	}
}

// MetaTxByNonce implements the sort interface to allow sorting a list of transactions
// by their nonces. This is usually only useful for sorting transactions from a
// single account, otherwise a nonce comparison doesn't make much sense.
type MetaTxByNonce []*TxRef

func (s MetaTxByNonce) Len() int           { return len(s) }
func (s MetaTxByNonce) Less(i, j int) bool { return s[i].nonce < s[j].nonce }
func (s MetaTxByNonce) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
