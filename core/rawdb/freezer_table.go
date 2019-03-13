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

package rawdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

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

type index struct {
	filenum uint16
	offset  uint64
}

const indexSize = 12

// unmarshallBinary deserializes binary b into the rawIndex entry.
func (i *index) unmarshalBinary(b []byte) error {
	i.filenum = binary.BigEndian.Uint16(b[:4])
	i.offset = binary.BigEndian.Uint64(b[4:12])
	return nil
}

// marshallBinary serializes the rawIndex entry into binary.
func (i *index) marshallBinary() []byte {
	b := make([]byte, indexSize)
	binary.BigEndian.PutUint16(b[:4], i.filenum)
	binary.BigEndian.PutUint64(b[4:12], i.offset)
	return b
}

// freezerTable represents a single chained data table within the freezer (e.g. blocks).
// It consists of a data file (snappy encoded arbitrary data blobs) and an index
// file (uncompressed 64 bit indices into the data file).
type freezerTable struct {
	head    *os.File            // File descriptor for the data head of the table
	files   map[uint16]*os.File // open files
	id      uint16              // number of the currently active head file
	offsets *os.File            // File descriptor for the index file of the table

	noCompression bool   // if true, disables snappy compression. Note: does not work retroactively
	items         uint64 // Number of items stored in the table
	bytes         uint64 // Number of head bytes stored in the table
	name          string
	path          string
	readMeter     metrics.Meter // Meter for measuring the effective amount of data read
	writeMeter    metrics.Meter // Meter for measuring the effective amount of data written

	logger         log.Logger   // Logger with database path and table name ambedded
	lock           sync.RWMutex // Mutex protecting the data file descriptors
	maxContentSize uint64       // Max file size for data-files

}

func newDefaultTable(path string, name string, readMeter metrics.Meter, writeMeter metrics.Meter) (*freezerTable, error) {
	return newTable(path, name, readMeter, writeMeter, 2*1000*1000*1000, false)
}

// newTable opens a freezer table, creating the data and index files if they are
// non existent. Both files are truncated to the shortest common length to ensure
// they don't go out of sync.
func newTable(path string, name string, readMeter metrics.Meter, writeMeter metrics.Meter, maxFilesize uint64, noCompression bool) (*freezerTable, error) {
	// Ensure the containing directory exists and open the index file
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
		offsets:        offsets,
		files:          make(map[uint16]*os.File),
		readMeter:      readMeter,
		writeMeter:     writeMeter,
		name:           name,
		path:           path,
		logger:         log.New("database", path, "table", name),
		noCompression:  noCompression,
		maxContentSize: maxFilesize,
	}
	if err := tab.repair(); err != nil {
		tab.Close()
		return nil, err
	}
	return tab, nil
}

// repair cross checks the head and the offsets file and truncates them to
// be in sync with each other after a potential crash / data loss.
func (t *freezerTable) repair() error {
	// Create a temporary offset buffer to init files with and read offsets into
	offset := make([]byte, indexSize)

	// If we've just created the files, initialize the offsets with the 0 index
	stat, err := t.offsets.Stat()
	if err != nil {
		return err
	}
	if stat.Size() == 0 {
		if _, err := t.offsets.Write(offset); err != nil {
			return err
		}
	}
	// Ensure the offsets are a multiple of indexSize bytes
	if overflow := stat.Size() % indexSize; overflow != 0 {
		t.offsets.Truncate(stat.Size() - overflow) // New file can't trigger this path
	}
	// Retrieve the file sizes and prepare for truncation
	if stat, err = t.offsets.Stat(); err != nil {
		return err
	}
	offsetsSize := stat.Size()
	// Open the head file
	var (
		lastIndex   index
		contentSize uint64
		contentExp  uint64
	)
	t.offsets.ReadAt(offset, offsetsSize-indexSize)
	lastIndex.unmarshalBinary(offset)
	t.head, err = t.getFile(lastIndex.filenum, os.O_RDWR|os.O_CREATE|os.O_APPEND)
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
		// Truncate the offsets to point within the head file
		if contentExp > contentSize {
			t.logger.Warn("Truncating dangling offsets", "indexed", common.StorageSize(contentExp), "stored", common.StorageSize(contentSize))
			if err := t.offsets.Truncate(offsetsSize - indexSize); err != nil {
				return err
			}
			offsetsSize -= indexSize
			t.offsets.ReadAt(offset, offsetsSize-indexSize)
			var newLastIndex index
			newLastIndex.unmarshalBinary(offset)
			// We might have slipped back into an earlier head-file here
			if newLastIndex.filenum != lastIndex.filenum {
				t.head, err = t.getFile(newLastIndex.filenum, os.O_RDWR|os.O_CREATE|os.O_APPEND)
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
	if err := t.offsets.Sync(); err != nil {
		return err
	}
	if err := t.head.Sync(); err != nil {
		return err
	}
	// Update the item and byte counters and return
	t.items = uint64(offsetsSize/indexSize - 1) // last index points to the end of the data file
	t.bytes = uint64(contentSize)

	t.logger.Debug("Chain freezer table opened", "items", t.items, "size", common.StorageSize(t.bytes))
	return nil
}

// truncate discards any recent data above the provided threashold number.
func (t *freezerTable) truncate(items uint64) error {
	// If out item count is corrent, don't do anything
	if t.items <= items {
		return nil
	}
	// Something's out of sync, truncate the table's offset index
	t.logger.Warn("Truncating freezer table", "items", t.items, "limit", items)
	if err := t.offsets.Truncate(int64(items+1) * 8); err != nil {
		return err
	}
	// Calculate the new expected size of the data file and truncate it
	offset := make([]byte, 8)
	t.offsets.ReadAt(offset, int64(items)*8)
	expected := binary.LittleEndian.Uint64(offset)

	if err := t.head.Truncate(int64(expected)); err != nil {
		return err
	}
	// All data files truncated, set internal counters and return
	t.items, t.bytes = items, expected
	return nil
}

// Close closes all opened files.
func (t *freezerTable) Close() error {
	t.lock.Lock()
	defer t.lock.Unlock()

	var errs []error
	if err := t.offsets.Close(); err != nil {
		errs = append(errs, err)
	}
	t.offsets = nil

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
func (t *freezerTable) getFile(num uint16, flag int) (f *os.File, err error) {
	var exist bool
	if f, exist = t.files[num]; !exist {
		var fname string
		if t.noCompression {
			fname = fmt.Sprintf("%s.%d.rdat", t.name, num)
		} else {
			fname = fmt.Sprintf("%s.%d.cdat", t.name, num)
		}
		f, err = os.OpenFile(filepath.Join(t.path, fname), flag, 0644)
		if err != nil {
			return nil, err
		}
		t.files[num] = f
	}
	return f, nil
}

// Append injects a binary blob at the end of the freezer table. The item index
// is a precautionary parameter to ensure data correctness, but the table will
// reject already existing data.
//
// Note, this method will *not* flush any data to disk so be sure to explicitly
// fsync before irreversibly deleting data from the database.
func (t *freezerTable) Append(item uint64, blob []byte) error {
	// Ensure the table is still accessible
	if t.offsets == nil || t.head == nil {
		return errClosed
	}
	// Ensure only the next item can be written, nothing else
	if t.items != item {
		panic(fmt.Sprintf("appending unexpected item: want %d, have %d", t.items, item))
	}
	// Encode the blob and write it into the data file
	if !t.noCompression {
		blob = snappy.Encode(nil, blob)
	}
	bLen := uint64(len(blob))
	if t.bytes+bLen > t.maxContentSize {

		// we need a new file, writing would overflow
		nextId := t.id + 1
		// We open the next file in truncated mode -- if this file already
		// exists, we need to start over from scratch on it
		content, err := t.getFile(nextId, os.O_RDWR|os.O_CREATE|os.O_TRUNC)
		if err != nil {
			return err
		}
		// Swap out the current file
		t.head = content
		t.bytes = 0
		t.id = nextId
	}
	t.head.Write(blob)
	t.bytes += bLen
	idx := index{
		filenum: t.id,
		offset:  t.bytes,
	}
	// Write offsets
	t.offsets.Write(idx.marshallBinary())
	t.writeMeter.Mark(int64(bLen + 12)) // 12 = 1 x 12 byte index
	t.items++
	return nil
}

// getOffsets returns the indexes for the item
func (t *freezerTable) getOffsets(item uint64) (*index, *index, error) {

	var start, end index
	indexdata := make([]byte, 12)
	if _, err := t.offsets.ReadAt(indexdata, int64(item*12)); err != nil {
		return nil, nil, err
	}
	start.unmarshalBinary(indexdata)
	if _, err := t.offsets.ReadAt(indexdata, int64((item+1)*12)); err != nil {
		return nil, nil, err
	}
	end.unmarshalBinary(indexdata)

	if start.filenum != end.filenum {
		// If a piece of data 'crosses' a data-file,
		// it's actually in one piece on the second data-file.
		// We return a zero-index for the second file as start
		start = index{
			filenum: end.filenum,
			offset:  0,
		}
	}
	return &start, &end, nil
}

// Retrieve looks up the data offset of an item with the given index and retrieves
// the raw binary blob from the data file.
func (t *freezerTable) Retrieve(item uint64) ([]byte, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	// Ensure the table and the item is accessible
	if t.offsets == nil || t.head == nil {
		return nil, errClosed
	}
	if t.items <= item {
		return nil, errOutOfBounds
	}
	start, end, err := t.getOffsets(item)
	if err != nil {
		return nil, err
	}
	dataFile, err := t.getFile(start.filenum, os.O_RDONLY)
	if err != nil {
		return nil, err
	}
	// Retrieve the data itself, decompress and return
	blob := make([]byte, end.offset-start.offset)
	if _, err := dataFile.ReadAt(blob, int64(start.offset)); err != nil {
		return nil, err
	}
	t.readMeter.Mark(int64(len(blob) + 2*indexSize)) // 16 = 2 x 8 byte offset

	if !t.noCompression {
		return snappy.Decode(nil, blob)
	} else {
		return blob, nil
	}
}

// Sync pushes any pending data from memory out to disk. This is an expensive
// operation, so use it with care.
func (t *freezerTable) Sync() error {
	if err := t.offsets.Sync(); err != nil {
		return err
	}
	return t.head.Sync()
}
