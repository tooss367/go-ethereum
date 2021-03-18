// Copyright 2018 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

// Package memorydb implements the key-value database layer based on memory maps.
package relaydb

import (
	"errors"

	"github.com/ethereum/go-ethereum/ethdb"
)

var (
	// errMemorydbClosed is returned if a memory database was already closed at the
	// invocation of a data access operation.
	errMemorydbClosed = errors.New("database closed")

	// errMemorydbNotFound is returned if a key is requested that is not found in
	// the provided memory database.
	errMemorydbNotFound = errors.New("not found")
)

// Database is an ephemeral key-value store. Apart from basic data storage
// functionality it also supports batch writes and iterating over the keyspace in
// binary-alphabetical order.
type Database struct {
	primary   ethdb.KeyValueStore
	secondary ethdb.KeyValueStore
	hits      int
	misses    int
}

// New returns a wrapped map with all the required database interface methods
// implemented.
func New(primary, secondary ethdb.KeyValueStore) *Database {
	return &Database{
		primary:   primary,
		secondary: secondary,
	}
}

// Close deallocates the internal map and ensures any consecutive data access op
// failes with an error.
func (db *Database) Close() error {
	//db.lock.Lock()
	//defer db.lock.Unlock()

	db.primary.Close()
	db.secondary.Close()
	db.primary = nil
	db.secondary = nil
	return nil
}

// Has retrieves if a key is present in the key-value store.
func (db *Database) Has(key []byte) (bool, error) {
	panic("Has not supported")
	//db.lock.RLock()
	//defer db.lock.RUnlock()
	//
	//if db.primary == nil {
	//	return false, errMemorydbClosed
	//}
	//if ok, _ := db.primary.Has(key); ok {
	//	return true
	//}
	//return db.secondary.Has(key)
}

// Get retrieves the given key if it's present in the key-value store.
func (db *Database) Get(key []byte) ([]byte, error) {
	//db.lock.RLock()
	//defer db.lock.RUnlock()

	if db.primary == nil {
		return nil, errMemorydbClosed
	}

	if k, err := db.primary.Get(key); err == nil {
		db.hits++ // NOT thread safe
		return k, err
	}
	db.misses++
	return db.secondary.Get(key)
}

// Put inserts the given value into the key-value store.
func (db *Database) Put(key []byte, value []byte) error {
	panic("Put not supported")
}

// Delete removes the key from the key-value store.
func (db *Database) Delete(key []byte) error {
	panic("Delete not supported")
}

// NewBatch creates a write-only key-value store that buffers changes to its host
// database until a final write is called.
func (db *Database) NewBatch() ethdb.Batch {
	panic("NewBatch not supported")
}

// NewIterator creates a binary-alphabetical iterator over a subset
// of database content with a particular key prefix, starting at a particular
// initial key (or after, if it does not exist).
func (db *Database) NewIterator(prefix []byte, start []byte) ethdb.Iterator {
	panic("Iteration is not supported")
}

// Stat returns a particular internal stat of the database.
func (db *Database) Stat(property string) (string, error) {
	panic("Stat not supported")
}

func (db *Database) Efficiency() (int, int){
	return db.hits, db.misses
}


// Compact is not supported on a memory database, but there's no need either as
// a memory database doesn't waste space anyway.
func (db *Database) Compact(start []byte, limit []byte) error {
	return nil
}
