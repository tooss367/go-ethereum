package eth

import (
	"github.com/ethereum/go-ethereum/consensus/ethash"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	fuzz "github.com/google/gofuzz"
	"time"
)

func getChain() *core.BlockChain {
	db := rawdb.NewMemoryDatabase()
	gspec := core.Genesis{
		Config: params.TestChainConfig,
		Alloc:  core.GenesisAlloc{},
	}
	genesis := gspec.MustCommit(db)
	blocks, _ := core.GenerateChain(gspec.Config, genesis, ethash.NewFaker(), db, 2,
		func(i int, gen *core.BlockGen) {})
	bc, _ := core.NewBlockChain(db, nil, gspec.Config, ethash.NewFaker(), vm.Config{}, nil, nil)

	if _, err := bc.InsertChain(blocks); err != nil {
		panic(err)
	}
	return bc
}

type dummyBackend struct {
	chain *core.BlockChain
}

func (d *dummyBackend) Chain() *core.BlockChain {
	return d.chain
}

func (d *dummyBackend) StateBloom() *trie.SyncBloom {
	return nil
}

func (d *dummyBackend) TxPool() TxPool {
	return nil
}

func (d *dummyBackend) AcceptTxs() bool {
	return true
}

func (d *dummyBackend) RunPeer(peer *Peer, handler Handler) error {
	return nil
}

func (d *dummyBackend) PeerInfo(id enode.ID) interface{} {
	return "Oy vey"
}

func (d *dummyBackend) Handle(peer *Peer, packet Packet) error {
	return nil
}

type dummyMsg struct {
	data []byte
}

func (d *dummyMsg) Decode(val interface{}) error {
	return rlp.DecodeBytes(d.data, val)
}

func (d *dummyMsg) Time() time.Time {
	return time.Now()
}

type dummyRW struct{}

func (d dummyRW) ReadMsg() (p2p.Msg, error) {
	return p2p.Msg{}, nil
}

func (d dummyRW) WriteMsg(msg p2p.Msg) error {
	return nil
}

func doFuzz(input []byte, obj interface{}, fuzzfunc msgHandler) int {
	bc := getChain()
	defer bc.Stop()
	backend := &dummyBackend{bc}
	peer := &Peer{id: "lalal", version: 65, rw: dummyRW{}}
	fuzz.NewFromGoFuzz(input).Fuzz(obj)
	data, _ := rlp.EncodeToBytes(obj)
	msg := &dummyMsg{data}
	handleGetBlockHeaders(backend, msg, peer)
	return 0
}

func FuzzA(input []byte) int { return doFuzz(input, &GetBlockHeadersPacket{}, handleGetBlockHeaders) }
func FuzzB(input []byte) int { return doFuzz(input, &GetReceiptsPacket{}, handleGetReceipts) }
func FuzzC(input []byte) int { return doFuzz(input, &GetBlockBodiesPacket{}, handleGetBlockBodies) }
func FuzzD(input []byte) int { return doFuzz(input, &GetNodeDataPacket{}, handleGetNodeData) }
