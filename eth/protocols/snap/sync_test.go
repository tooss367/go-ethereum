package snap

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/trie"
	"testing"
)

type testPeer struct {
	id   string
	test *testing.T
	remote *Syncer
	log log.Logger
}

func (t *testPeer) ID() string {
	return t.id
}

func (t *testPeer) RequestAccountRange(requestId uint64, root common.Hash, origin common.Hash, bytes uint64) error {
	t.test.Logf("%v <- RequestAccountRange(%x, %x, %d)", requestId, root, origin, bytes)

	// Pass the response
	go func(){
		var hashes []common.Hash
		var accounts [][]byte
		var proofs [][]byte

		t.remote.OnAccounts(t, requestId, hashes, accounts, proofs)
	}()
	return nil
}

func (t *testPeer) RequestStorageRange(id uint64, root common.Hash, account common.Hash, origin common.Hash, bytes uint64) error {
	panic("implement me")
}

func (t *testPeer) RequestByteCodes(id uint64, hashes []common.Hash, bytes uint64) error {
	panic("implement me")
}

func (t *testPeer) Log() log.Logger {
	return t.log
}

func newTestPeer(id string, t *testing.T, syncer *Syncer) *testPeer {
	return &testPeer{
		id:   id,
		test: t,
		remote: syncer,
		log: log.New("id",id),
	}
}

type snapTester struct {
	//genesis *types.Block   // Genesis blocks used by the tester and peers
	stateDb ethdb.Database // Database used by the tester for syncing from peers
}

func TestSync(t *testing.T) {
	stateDb := rawdb.NewMemoryDatabase()
	stateBloom := trie.NewSyncBloom(1, stateDb)
	syncer := NewSyncer(stateDb, stateBloom)

	source := newTestPeer("source", t, syncer)
	//sink := newTestPeer()


	cancel := make(chan struct{})
	syncer.Register(source)
	if err := syncer.Sync(common.Hash{}, cancel); err != nil {
		t.Fatalf("failed to start sync: %v", err)
	}
}
