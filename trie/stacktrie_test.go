package trie

import (
	"bytes"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
)

func TestSizeBug(t *testing.T) {
	st := NewStackTrie(nil)
	nt, _ := New(common.Hash{}, NewDatabase(memorydb.New()))

	leaf := common.FromHex("290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563")
	value := common.FromHex("94cf40d0d2b44f2b66e07cace1372ca42b73cf21a3")

	nt.TryUpdate(leaf, value)
	st.TryUpdate(leaf, value)

	if nt.Hash() != st.Hash() {
		t.Fatalf("error %x != %x", st.Hash(), nt.Hash())
	}
}

func TestEmptyBug(t *testing.T) {
	st := NewStackTrie(nil)
	nt, _ := New(common.Hash{}, NewDatabase(memorydb.New()))

	//leaf := common.FromHex("290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563")
	//value := common.FromHex("94cf40d0d2b44f2b66e07cace1372ca42b73cf21a3")
	kvs := []struct {
		K string
		V string
	}{
		{K: "405787fa12a823e0f2b7631cc41b3ba8828b3321ca811111fa75cd3aa3bb5ace", V: "9496f4ec2bf9dab484cac6be589e8417d84781be08"},
		{K: "40edb63a35fcf86c08022722aa3287cdd36440d671b4918131b2514795fefa9c", V: "01"},
		{K: "b10e2d527612073b26eecdfd717e6a320cf44b4afac2b0732d9fcbe2b7fa0cf6", V: "947a30f7736e48d6599356464ba4c150d8da0302ff"},
		{K: "c2575a0e9e593c00f959f8c92f12db2869c3395a3b0502d05e2516446f71f85b", V: "02"},
	}

	for _, kv := range kvs {
		nt.TryUpdate(common.FromHex(kv.K), common.FromHex(kv.V))
		st.TryUpdate(common.FromHex(kv.K), common.FromHex(kv.V))
	}

	if nt.Hash() != st.Hash() {
		t.Fatalf("error %x != %x", st.Hash(), nt.Hash())
	}
}

func TestValLength56(t *testing.T) {
	st := NewStackTrie(nil)
	nt, _ := New(common.Hash{}, NewDatabase(memorydb.New()))

	//leaf := common.FromHex("290decd9548b62a8d60345a988386fc84ba6bc95484008f6362f93160ef3e563")
	//value := common.FromHex("94cf40d0d2b44f2b66e07cace1372ca42b73cf21a3")
	kvs := []struct {
		K string
		V string
	}{
		{K: "405787fa12a823e0f2b7631cc41b3ba8828b3321ca811111fa75cd3aa3bb5ace", V: "1111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111111"},
	}

	for _, kv := range kvs {
		nt.TryUpdate(common.FromHex(kv.K), common.FromHex(kv.V))
		st.TryUpdate(common.FromHex(kv.K), common.FromHex(kv.V))
	}

	if nt.Hash() != st.Hash() {
		t.Fatalf("error %x != %x", st.Hash(), nt.Hash())
	}
}

func genTxs(num uint64) (types.Transactions, error) {
	key, err := crypto.HexToECDSA("deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	if err != nil {
		return nil, err
	}
	var addr = crypto.PubkeyToAddress(key.PublicKey)
	newTx := func(i uint64) (*types.Transaction, error) {
		signer := types.NewEIP155Signer(big.NewInt(18))
		tx, err := types.SignTx(types.NewTransaction(i, addr, new(big.Int), 0, new(big.Int).SetUint64(10000000), nil), signer, key)
		return tx, err
	}
	var txs types.Transactions
	for i := uint64(0); i < num; i++ {
		tx, err := newTx(i)
		if err != nil {
			return nil, err
		}
		txs = append(txs, tx)
	}
	return txs, nil
}

func TestDeriveSha(t *testing.T) {
	txs, err := genTxs(0)
	if err != nil {
		t.Fatal(err)
	}
	for len(txs) < 1000 {
		exp := types.DeriveSha(txs, newEmpty())
		got := types.DeriveSha(txs, NewStackTrie(nil))
		if !bytes.Equal(got[:], exp[:]) {
			t.Fatalf("%d txs: got %x exp %x", len(txs), got, exp)
		}
		newTxs, err := genTxs(uint64(len(txs) + 1))
		if err != nil {
			t.Fatal(err)
		}
		txs = append(txs, newTxs...)
	}
}

func BenchmarkDeriveSha200(b *testing.B) {
	txs, err := genTxs(200)
	if err != nil {
		b.Fatal(err)
	}
	var exp common.Hash
	var got common.Hash
	b.Run("std_trie", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			exp = types.DeriveSha(txs, newEmpty())
		}
	})

	b.Run("stack_trie", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			got = types.DeriveSha(txs, NewStackTrie(nil))
		}
	})
	if got != exp {
		b.Errorf("got %x exp %x", got, exp)
	}
}
