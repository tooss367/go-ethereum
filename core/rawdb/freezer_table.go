// Copyright 2019 The go-ethereum Authors
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

package rawdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/golang/snappy"
)

var (
	// errClosed is returned if an operation attempts to read from or write to the
	// freezer table after it has already been closed.
	errClosed = errors.New("closed")

	// errOutOfBounds is returned if the item requested is not contained within the
	// freezer table.
	errOutOfBounds = errors.New("out of bounds")
)

// indexEntry contains the number/id of the file that the data resides in, aswell as the
// offset within the file to the end of the data
// In serialized form, the filenum is stored as uint16.
type indexEntry struct {
	filenum uint32
	offset  uint64
}

const indexEntrySize = 12

// unmarshallBinary deserializes binary b into the rawIndex entry.
func (i *indexEntry) unmarshalBinary(b []byte) error {
	i.filenum = uint32(binary.BigEndian.Uint16(b[:4]))
	i.offset = binary.BigEndian.Uint64(b[4:12])
	return nil
}

// marshallBinary serializes the rawIndex entry into binary.
func (i *indexEntry) marshallBinary() []byte {
	b := make([]byte, indexEntrySize)
	binary.BigEndian.PutUint16(b[:4], uint16(i.filenum))
	binary.BigEndian.PutUint64(b[4:12], i.offset)
	return b
}

// freezerTable represents a single chained data table within the freezer (e.g. blocks).
// It consists of a data file (snappy encoded arbitrary data blobs) and an indexEntry
// file (uncompressed 64 bit indices into the data file).
type freezerTable struct {
	noCompression bool   // if true, disables snappy compression. Note: does not work retroactively
	maxFileSize   uint64 // Max file size for data-files
	name          string
	path          string

	head   *os.File            // File descriptor for the data head of the table
	files  map[uint32]*os.File // open files
	headId uint32              // number of the currently active head file
	index  *os.File            // File descriptor for the indexEntry file of the table

	items      uint64        // Number of items stored in the table
	headBytes  uint64        // Number of bytes written to the head file
	readMeter  metrics.Meter // Meter for measuring the effective amount of data read
	writeMeter metrics.Meter // Meter for measuring the effective amount of data written

	logger log.Logger   // Logger with database path and table name ambedded
	lock   sync.RWMutex // Mutex protecting the data file descriptors
}

// newTable opens a freezer table with default settings - 2G files and snappy compression
func newTable(path string, name string, readMeter metrics.Meter, writeMeter metrics.Meter) (*freezerTable, error) {
	return newCustomTable(path, name, readMeter, writeMeter, 2*1000*1000*1000, false)
}

// newCustomTable opens a freezer table, creating the data and index files if they are
// non existent. Both files are truncated to the shortest common length to ensure
// they don't go out of sync.
func newCustomTable(path string, name string, readMeter metrics.Meter, writeMeter metrics.Meter, maxFilesize uint64, noCompression bool) (*freezerTable, error) {
	// Ensure the containing directory exists and open the indexEntry file
	if err := os.MkdirAll(path, 0755); err != nil {
		return nil, err
	}
	var idxName string
	if noCompression {
		// raw idx
		idxName = fmt.Sprintf("%s.ridx", name)
	} else {
		// compressed idx
		idxName = fmt.Sprintf("%s.cidx", name)
	}
	offsets, err := os.OpenFile(filepath.Join(path, idxName), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
	if err != nil {
		return nil, err
	}
	// Create the table and repair any past inconsistency
	tab := &freezerTable{
		index:         offsets,
		files:         make(map[uint32]*os.File),
		readMeter:     readMeter,
		writeMeter:    writeMeter,
		name:          name,
		path:          path,
		logger:        log.New("database", path, "table", name),
		noCompression: noCompression,
		maxFileSize:   maxFilesize,
	}
	if err := tab.repair(); err != nil {
		tab.Close()
		return nil, err
	}
	return tab, nil
}

// repair cross checks the head and the index file and truncates them to
// be in sync with each other after a potential crash / data loss.
func (t *freezerTable) repair() error {
	// Create a temporary offset buffer to init files with and read indexEntry into
	buffer := make([]byte, indexEntrySize)

	// If we've just created the files, initialize the index with the 0 indexEntry
	stat, err := t.index.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		if _, err := t.index.Write(buffer); err != nil {
			return err
		}
	}
	// Ensure the index is a multiple of indexEntrySize bytes
	if overflow := stat.Size() % indexEntrySize; overflow != 0 {
		t.index.Truncate(stat.Size() - overflow) // New file can't trigger this path
	}
	// Retrieve the file sizes and prepare for truncation
	if stat, err = t.index.Stat(); err != nil {
		return err
	}
	offsetsSize := stat.Size()

	// Open the head file
	var (
		lastIndex   indexEntry
		contentSize uint64
		contentExp  uint64
	)
	t.index.ReadAt(buffer, offsetsSize-indexEntrySize)
	lastIndex.unmarshalBinary(buffer)
	t.head, err = t.getFileInternal(lastIndex.filenum, os.O_RDWR|os.O_CREATE|os.O_APPEND)
	if err != nil {
		return err
	}
	if stat, err = t.head.Stat(); err != nil {
		return err
	}
	contentSize = uint64(stat.Size())

	// Keep truncating both files until they come in sync
	contentExp = lastIndex.offset

	for contentExp != contentSize {
		// Truncate the head file to the last offset pointer
		if contentExp < contentSize {
			t.logger.Warn("Truncating dangling head", "indexed", common.StorageSize(contentExp), "stored", common.StorageSize(contentSize))
			if err := t.head.Truncate(int64(contentExp)); err != nil {
				return err
			}
			contentSize = contentExp
		}
		// Truncate the index to point within the head file
		if contentExp > contentSize {
			t.logger.Warn("Truncating dangling indexes", "indexed", common.StorageSize(contentExp), "stored", common.StorageSize(contentSize))
			if err := t.index.Truncate(offsetsSize - indexEntrySize); err != nil {
				return err
			}
			offsetsSize -= indexEntrySize
			t.index.ReadAt(buffer, offsetsSize-indexEntrySize)
			var newLastIndex indexEntry
			newLastIndex.unmarshalBinary(buffer)
			// We might have slipped back into an earlier head-file here
			if newLastIndex.filenum != lastIndex.filenum {
				// release earlier opened file
				t.releaseFile(lastIndex.filenum)
				t.head, err = t.getFileInternal(newLastIndex.filenum, os.O_RDWR|os.O_CREATE|os.O_APPEND)
				if stat, err = t.head.Stat(); err != nil {
					// TODO, anything more we can do here?
					// A data file has gone missing...
					return err
				}
				contentSize = uint64(stat.Size())
			}
			lastIndex = newLastIndex
			contentExp = lastIndex.offset
		}
	}
	// Ensure all reparation changes have been written to disk
	if err := t.index.Sync(); err != nil {
		return err
	}
	if err := t.head.Sync(); err != nil {
		return err
	}
	// Update the item and byte counters and return
	t.items = uint64(offsetsSize/indexEntrySize - 1) // last indexEntry points to the end of the data file
	t.headBytes = uint64(contentSize)
	t.headId = lastIndex.filenum
	t.logger.Debug("Chain freezer table opened", "items", t.items, "size", common.StorageSize(t.headBytes))
	return nil
}

// truncate discards any recent data above the provided threashold number.
func (t *freezerTable) truncate(items uint64) error {
	t.lock.Lock()
	defer t.lock.Unlock()
	// If out item count is corrent, don't do anything
	if atomic.LoadUint64(&t.items) <= items {
		return nil
	}
	// Something's out of sync, truncate the table's offset index
	t.logger.Warn("Truncating freezer table", "items", t.items, "limit", items)
	if err := t.index.Truncate(int64(items+1) * indexEntrySize); err != nil {
		return err
	}
	// Calculate the new expected size of the data file and truncate it
	buffer := make([]byte, indexEntrySize)
	if _, err := t.index.ReadAt(buffer, int64(items*indexEntrySize)); err != nil {
		return err
	}
	var expected indexEntry
	expected.unmarshalBinary(buffer)
	// We might need to truncate back to older files
	if expected.filenum != t.headId {
		// If already open for reading, force-reopen for writing
		t.releaseFile(expected.filenum)
		newHead, err := t.getFileInternal(expected.filenum, os.O_RDWR|os.O_CREATE|os.O_APPEND)
		if err != nil {
			return err
		}
		// release any files _after the current head -- both the previous head
		// and any files which may have been opened for reading
		t.releaseFilesAfter(expected.filenum)
		// set back the historic head
		t.head = newHead
		atomic.StoreUint32(&t.headId, expected.filenum)
	}

	if err := t.head.Truncate(int64(expected.offset)); err != nil {
		return err
	}
	// All data files truncated, set internal counters and return
	atomic.StoreUint64(&t.items, items)
	atomic.StoreUint64(&t.headBytes, expected.offset)
	return nil
}

// Close closes all opened files.
func (t *freezerTable) Close() error {
	t.lock.Lock()
	defer t.lock.Unlock()

	var errs []error
	if err := t.index.Close(); err != nil {
		errs = append(errs, err)
	}
	t.index = nil

	for _, f := range t.files {
		if err := f.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	t.head = nil

	if errs != nil {
		return fmt.Errorf("%v", errs)
	}
	return nil
}

// getFile either returns an existing handle, or opens the file with the
// given flags (and stores the handle for later)
// This method assumes that caller has readlock on t.lock
func (t *freezerTable) getFile(num uint32, flag int) (*os.File, error) {
	if f, exist := t.files[num]; exist {
		return f, nil
	}
	// Release rlock, to obtain wlock
	t.lock.RUnlock()
	t.lock.Lock()
	f, e := t.getFileInternal(num, flag)
	// Set the rlock back again
	t.lock.Unlock()
	t.lock.RLock()
	return f, e
}

// getFileInternal assumes that the write-lock is held by the caller
func (t *freezerTable) getFileInternal(num uint32, flag int) (f *os.File, err error) {
	var exist bool
	if f, exist = t.files[num]; !exist {
		var name string
		if t.noCompression {
			name = fmt.Sprintf("%s.%d.rdat", t.name, num)
		} else {
			name = fmt.Sprintf("%s.%d.cdat", t.name, num)
		}
		f, err = os.OpenFile(filepath.Join(t.path, name), flag, 0644)
		if err != nil {
			return nil, err
		}
		t.files[num] = f
	}
	return f, err
}

// releaseFile closes a file, and removes it from the open file cache.
// Assumes that the caller holds the write lock
func (t *freezerTable) releaseFile(num uint32) {
	if f, exist := t.files[num]; exist {
		delete(t.files, num)
		f.Close()
	}
}

// releaseFilesAfter closes all open files with a higher number
func (t *freezerTable) releaseFilesAfter(num uint32) {
	for fnum, f := range t.files {
		if fnum > num {
			delete(t.files, fnum)
			f.Close()
		}
	}
}

// Append injects a binary blob at the end of the freezer table. The item number
// is a precautionary parameter to ensure data correctness, but the table will
// reject already existing data.
//
// Note, this method will *not* flush any data to disk so be sure to explicitly
// fsync before irreversibly deleting data from the database.
func (t *freezerTable) Append(item uint64, blob []byte) error {
	// Ensure the table is still accessible
	if t.index == nil || t.head == nil {
		return errClosed
	}
	// Ensure only the next item can be written, nothing else
	if atomic.LoadUint64(&t.items) != item {
		panic(fmt.Sprintf("appending unexpected item: want %d, have %d", t.items, item))
	}
	// Encode the blob and write it into the data file
	if !t.noCompression {
		blob = snappy.Encode(nil, blob)
	}
	bLen := uint64(len(blob))
	if t.headBytes+bLen > t.maxFileSize {
		t.lock.Lock()
		// we need a new file, writing would overflow
		nextId := atomic.LoadUint32(&t.headId) + 1
		// We open the next file in truncated mode -- if this file already
		// exists, we need to start over from scratch on it
		newHead, err := t.getFileInternal(nextId, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			t.lock.Unlock()
			return err
		}
		// Close old file. It will be reopened in RDONLY mode if needed
		t.releaseFile(t.headId)
		// Swap out the current head
		t.head = newHead
		atomic.StoreUint64(&t.headBytes, 0)
		atomic.StoreUint32(&t.headId, nextId)
		t.lock.Unlock()
	}
	if _, err := t.head.Write(blob); err != nil {
		return err
	}
	newOffset := atomic.AddUint64(&t.headBytes, bLen)
	idx := indexEntry{
		filenum: atomic.LoadUint32(&t.headId),
		offset:  newOffset,
	}
	// Write indexEntry
	t.index.Write(idx.marshallBinary())
	t.writeMeter.Mark(int64(bLen + indexEntrySize))
	atomic.AddUint64(&t.items, 1)
	return nil
}

// getOffsets returns the indexes for the item
// returns start, end, filenumber and error
func (t *freezerTable) getOffsets(item uint64) (uint64, uint64, uint32, error) {
	var startIdx, endIdx indexEntry
	buffer := make([]byte, indexEntrySize)
	if _, err := t.index.ReadAt(buffer, int64(item*indexEntrySize)); err != nil {
		return 0, 0, 0, err
	}
	startIdx.unmarshalBinary(buffer)
	if _, err := t.index.ReadAt(buffer, int64((item+1)*indexEntrySize)); err != nil {
		return 0, 0, 0, err
	}
	endIdx.unmarshalBinary(buffer)
	if startIdx.filenum != endIdx.filenum {
		// If a piece of data 'crosses' a data-file,
		// it's actually in one piece on the second data-file.
		// We return a zero-indexEntry for the second file as start
		return 0, endIdx.offset, endIdx.filenum, nil
	}
	return startIdx.offset, endIdx.offset, endIdx.filenum, nil
}

// Retrieve looks up the data offset of an item with the given number and retrieves
// the raw binary blob from the data file.
func (t *freezerTable) Retrieve(item uint64) ([]byte, error) {

	// Ensure the table and the item is accessible
	if t.index == nil || t.head == nil {
		return nil, errClosed
	}
	if atomic.LoadUint64(&t.items) <= item {
		return nil, errOutOfBounds
	}
	t.lock.RLock()
	startOffset, endOffset, filenum, err := t.getOffsets(item)
	if err != nil {
		return nil, err
	}
	dataFile, err := t.getFile(filenum, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	// Retrieve the data itself, decompress and return
	blob := make([]byte, endOffset-startOffset)
	if _, err := dataFile.ReadAt(blob, int64(startOffset)); err != nil {
		t.lock.RUnlock()
		return nil, err
	}
	t.lock.RUnlock()
	t.readMeter.Mark(int64(len(blob) + 2*indexEntrySize))

	if !t.noCompression {
		return snappy.Decode(nil, blob)
	} else {
		return blob, nil
	}
}

// Sync pushes any pending data from memory out to disk. This is an expensive
// operation, so use it with care.
func (t *freezerTable) Sync() error {
	if err := t.index.Sync(); err != nil {
		return err
	}
	return t.head.Sync()
}
