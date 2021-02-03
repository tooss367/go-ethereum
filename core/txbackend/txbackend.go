package txbackend

import (
	"github.com/ethereum/go-ethereum/log"
	"sync"
)

type TxBackend interface {
	Get(uint64) []byte
	Put([]byte) uint64
}

type MemoryBackend struct {
	data [][]byte
	mu   sync.RWMutex
}

func NewMemoryBackend() TxBackend {
	return &MemoryBackend{}
}
func (m *MemoryBackend) Get(key uint64) []byte {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if key > uint64(len(m.data)) {
		log.Error("Out of bounds", "query", key, "max", len(m.data))
		return nil
	}
	return m.data[key]
}

func (m *MemoryBackend) Put(blob []byte) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data = append(m.data, blob)
	return uint64(len(m.data))
}
