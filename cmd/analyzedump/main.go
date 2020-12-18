package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

func main() {
	//doSort()
	doMerge()
}

type fileIterator struct {
	f    *os.File
	next []byte
}

func (it *fileIterator) Next() bool {
	if it.f == nil {
		return false
	}
	n := make([]byte, 32)
	_, err := it.f.Read(n)
	if err != nil {
		it.f.Close()
		it.f = nil
		return false
	}
	it.next = n
	return true
}

func (it *fileIterator) Value() []byte {
	return it.next
}

type mergedIterator struct {
	files []*fileIterator
	next  []byte
}

func (it *mergedIterator) Init() {
	for _, f := range it.files {
		f.Next()
	}

}

func (it *mergedIterator) Next() bool {
	var nextIt *fileIterator
	var lowest []byte
	var index = -1
	if len(it.files) == 0 {
		return false
	}
	for i, f := range it.files {
		val := f.Value()
		if lowest == nil {
			lowest = val
			nextIt = f
			index = i
		} else if bytes.Compare(lowest, val) >= 0 {
			lowest = val
			nextIt = f
			index = i
		}
	}
	// Advance it
	if !nextIt.Next() {
		// EOF -- remove it
		it.files = append(it.files[:index], it.files[index+1:]...)
	}
	it.next = lowest
	return true
}

func (it *mergedIterator) Value() []byte {
	return it.next
}

func doMerge() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Need filename")
		os.Exit(1)
	}
	// Create a list of files to merge
	var files []*fileIterator
	basename := os.Args[1]
	var cont = true
	for i := 0; cont; i++ {
		cont = false
		f, err := os.Open(fmt.Sprintf("%v.sorted.%d", basename, i))
		if err == nil {
			files = append(files, &fileIterator{f, nil})
			cont = true
		}
		f2, err := os.Open(fmt.Sprintf("%v.code.sorted.%d", basename, i))
		if err == nil {
			files = append(files, &fileIterator{f2, nil})
			cont = true
		}
	}
	fmt.Printf("Opened %d files\n", len(files))
	it := mergedIterator{files, nil}
	it.Init()

	merged, err := os.Create(fmt.Sprintf("%v.merged", basename))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't open merged: %v\n", err)
		return
	}
	for it.Next() {
		merged.Write(it.Value())
	}
	merged.Close()
}

func doSort() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Need filename")
		os.Exit(1)
	}
	keyFile, err := os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Err: %v\n", err)
		os.Exit(1)
	}

	if err := uniq(keyFile, os.Args[1], 32); err != nil {
		fmt.Fprintf(os.Stderr, "Could not unquify: %v", err)
		keyFile.Close()
		os.Exit(1)
	}
	keyFile.Close()
	codeName := fmt.Sprintf("%s.code", os.Args[1])
	codeKeyFile, err := os.Open(codeName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Err: %v\n", err)
		os.Exit(1)
	}

	if err := uniq(codeKeyFile, codeName, 33); err != nil {
		fmt.Fprintf(os.Stderr, "Could not unquify: %v", err)
		codeKeyFile.Close()
		os.Exit(1)
	}
	codeKeyFile.Close()

}

type key []byte
type keyList [][]byte

func (k keyList) Len() int {
	return len(k)
}

func (k keyList) Less(i, j int) bool {
	return bytes.Compare(k[i], k[j]) < 0
}

func (k keyList) Swap(i, j int) {
	k[i], k[j] = k[j], k[i]
}

func uniq(f *os.File, fName string, keyLen int) error {
	//type key [32]byte
	var unsorted keyList
	var num = 0
	var flush = func() error {
		fmt.Printf("Sorting %v entries\n", len(unsorted))
		sort.Sort(unsorted)
		outName := fmt.Sprintf("%v.sorted.%d", fName, num)
		fmt.Printf("Flushing %d entries to %v\n", len(unsorted), outName)
		// Dump to an outfile
		outFile, err := os.Create(outName)
		if err != nil {
			return err
		}
		defer outFile.Close()
		for _, elem := range unsorted {
			outFile.Write(elem)
		}
		num++
		unsorted = unsorted[:0]
		return nil
	}

	for {
		k := make([]byte, keyLen)
		_, err := f.Read(k)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if keyLen == 33 {
			k2 := make([]byte, 32)
			copy(k2, k[1:])
			k = k2
		}
		unsorted = append(unsorted, k)
		// Sort every GB
		if len(unsorted) > 40*1000*1000 {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := flush(); err != nil {
		return err
	}
	return nil
}
