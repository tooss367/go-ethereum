package trie

import (
	"fmt"
	"github.com/allegro/bigcache"
	"testing"
	"time"
	crand "crypto/rand"
)

func TestCollisions(t *testing.T) {

	cleans, _ := bigcache.NewBigCache(bigcache.Config{
		Shards:             1024,
		LifeWindow:         time.Hour,
		MaxEntriesInWindow: 4096 * 1024,
		MaxEntrySize:       512,
		HardMaxCacheSize:   4096,
	})
	entries := make(map[string]bool)
	i := uint64(0)
	hash := make([]byte, 64)
	for i = 0; i < 10000000; i++ {

		crand.Read(hash)
		key := string(hash[:])
		if _, exist := entries[key]; !exist {
			// entry not stored
			if v, err := cleans.Get(key); err == nil {
				// Collision found
				t.Errorf("collision found, %x == %x", hash, v)
			}
			// store it
			cleans.Set(key, hash)
			entries[key] = true
		}
		if i  % 1000000 == 0{
			fmt.Printf("%+v\n", cleans.Stats())
		}
	}
	fmt.Printf("Continuing without storing, after storing %d items\n", i)
	for ; i < 1000000000; i++ {

		crand.Read(hash)
		key := string(hash[:])
		if _, exist := entries[key]; !exist {
			// entry not stored
			if v, err := cleans.Get(key); err == nil {
				// Collision found
				t.Errorf("collision found, %x == %x", hash, v)
			}
		}
		if i  % 1000000 == 0{
			fmt.Printf("%+v\n", cleans.Stats())
		}
	}
}
