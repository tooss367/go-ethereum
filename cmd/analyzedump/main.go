package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/png"
	"io"
	"math/bits"
	"math/rand"
	"os"
	"sort"
	"strings"
	"time"

	bloomfilter "github.com/holiman/bloomfilter/v2"
)

func main() {
	//doSort()
	//	doMerge()
	//doSquash()
	//checkFnv()
	//convertBloom()
	testBloom()
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

func doSquash() error {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Need filename")
		os.Exit(1)
	}
	var (
		inputFile  *os.File
		outputFile *os.File
		err        error
	)
	// Create a list of files to merge
	basename := os.Args[1]
	inputFile, err = os.Open(fmt.Sprintf("%v.merged", basename))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}
	defer inputFile.Close()
	outputFile, err = os.Create(fmt.Sprintf("%v.merged.uniq", basename))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}
	defer outputFile.Close()
	prev := make([]byte, 32)
	key := make([]byte, 32)
	var skipped uint64
	var written uint64
	for {
		_, err := inputFile.Read(key)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		if bytes.Equal(prev, key) {
			skipped++
			continue
		}
		copy(prev, key)
		outputFile.Write(key)
		written++
	}
	fmt.Printf("Wrote %d entries, skipped %d entries\n", written, skipped)
	return nil
}

func checkFnv() error {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Need filename")
		os.Exit(1)
	}
	var (
		inputFile *os.File
		err       error
	)
	inputFile, err = os.Open(fmt.Sprintf("%v.merged.uniq", os.Args[1]))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		os.Exit(1)
	}
	defer inputFile.Close()
	key := make([]byte, 32)
	hasher := fnv.New64a()
	var sum1 = func(k []byte) uint64 {
		hasher.Reset()
		hasher.Write(k)
		return hasher.Sum64()
	}
	var sum2 = func(k []byte) uint64 {
		return binary.BigEndian.Uint64(k)
	}
	var a uint64
	var b uint64
	{
		search, err := hex.DecodeString("bf5c69e35f60242e98657683d31b3ee7f90dbe0f649c75b5bfaffa96c966d638")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v", err)
			return nil
		}
		a = sum1(search)
		b = sum2(search)
	}
	fmt.Printf("fnv hash: %v\n", a)
	fmt.Printf("kck hash: %v\n", b)

	var fnv *bloomfilter.Filter
	var kck *bloomfilter.Filter

	// 2 GB
	fnv, err = bloomfilter.New(2048*1024*1024*8, 4)
	//fnv, err = bloomfilter.New(256, 4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		return nil
	}
	// 2 GB
	kck, err = bloomfilter.New(2048*1024*1024*8, 4)
	//kck, err = bloomfilter.New(256, 4)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v", err)
		return nil
	}
	t := time.Now()
	i := 0
	for {
		_, err := inputFile.Read(key)
		if errors.Is(err, io.EOF) {
			break
		}
		fnvHash := sum1(key)
		kckHash := sum2(key)
		if fnvHash == a {
			fmt.Printf("Duplicate fnv: %x (fnvhash %d kckhash %d)\n", key, fnvHash, kckHash)
		}
		if kckHash == b {
			fmt.Printf("Duplicate kck: %x (fnvhash %d kckhash %d)\n", key, fnvHash, kckHash)
		}
		fnv.AddHash(fnvHash)
		kck.AddHash(kckHash)
		i++
		if time.Since(t) > 10*time.Second {
			fmt.Printf("%d done\n", i)
			t = time.Now()
		}
	}
	fmt.Printf("fnv FP probability: %v\n", fnv.FalsePosititveProbability())
	fmt.Printf("kck FP probability: %v\n", kck.FalsePosititveProbability())

	fmt.Printf("fnv filled-ratio: %v\n", fnv.PreciseFilledRatio())
	fmt.Printf("kck filled-ratio: %v\n", kck.PreciseFilledRatio())

	if fnv.ContainsHash(a) {
		fmt.Printf("FNV bloom filter produces 'hit' for the stateroot\n")
	} else {
		fmt.Printf("FNV bloom filter does NOT produce 'hit' for the stateroot\n")
	}
	if kck.ContainsHash(b) {
		fmt.Printf("Keccak bloom filter produces 'hit' for the stateroot\n")
	} else {
		fmt.Printf("Keccak bloom filter does NOT produce 'hit' for the stateroot\n")
	}
	fnv.WriteFile("fnv.bloom.gz")
	kck.WriteFile("kck.bloom.gz")
	return nil
}

func convertBloom() error {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Need filename\n")
		os.Exit(1)
	}
	if !strings.HasSuffix(os.Args[1], "gz") {
		fmt.Fprintf(os.Stderr, "not a bloom?\n")
		os.Exit(1)
	}
	var (
		err    error
		f      *os.File
		reader *gzip.Reader
	)
	f, err = os.Open(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	defer f.Close()

	reader, err = gzip.NewReader(f)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	defer reader.Close()
	// 2Gb -> 2048 * 1024 pixels,
	// 2M pixels
	// 1024 bits per pixesl: 128 bytes
	img := image.NewGray16(image.Rect(0, 0, 2048, 1024))
	for y := 0; y < 1024; y++ {
		for x := 0; x < 2048; x++ {
			sum := 0
			val := make([]byte, 128)
			reader.Read(val)
			for _, v := range val {
				sum += bits.OnesCount8(v)
			}
			col := uint64(0xFFFF) * uint64(sum) / uint64(1024)
			img.SetGray16(x, y, color.Gray16{Y: uint16(col)})
		}
	}
	var out *os.File
	out, err = os.Create(fmt.Sprintf("%v.png", os.Args[1]))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	defer out.Close()
	err = png.Encode(out, img)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	return nil
}

func testBloom() error {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Need filename\n")
		os.Exit(1)
	}
	if !strings.HasSuffix(os.Args[1], "gz") {
		fmt.Fprintf(os.Stderr, "not a bloom?\n")
		os.Exit(1)
	}
	f, _, err := bloomfilter.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	fmt.Printf("Bloom functions: %v\n", f.K())
	fmt.Printf("Bloom bits: %v\n", f.M())
	hits := 0
	numTests := 100000
	for i := 0; i < numTests; i++ {
		if f.ContainsHash(rand.Uint64()) {
			hits++
		}
	}
	fmt.Printf("Hit rate (100K random tests): %02f %%\n", 100*float64(hits)/float64(numTests))
	return nil
}
