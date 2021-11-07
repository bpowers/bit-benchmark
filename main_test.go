// Copyright 2021 The bit Authors. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package bitbenchmark

import (
	"bufio"
	"bytes"
	"os"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/bpowers/bit"
	"github.com/bsm/go-sparkey"
)

var (
	benchTableOnce    sync.Once
	benchTableBit     *bit.Table
	benchTableSparkey *sparkey.HashReader
	benchHashmap      map[string]string
	benchEntries      []benchEntry
)

type benchEntry struct {
	Key   string
	Value string
}

var keyBuf []byte
var valueBuf []byte

func loadBenchTable() {
	var err error
	var expected map[string]string
	benchTableBit, benchTableSparkey, expected, err = openTestFile("testdata.large")
	if err != nil {
		panic(err)
	}

	for k, v := range expected {
		benchEntries = append(benchEntries, benchEntry{Key: k, Value: v})
	}

	benchHashmap = make(map[string]string)
	for _, entry := range benchEntries {
		keyBuf = make([]byte, len(entry.Key))
		copy(keyBuf, entry.Key)
		valueBuf = make([]byte, len(entry.Value))
		copy(valueBuf, entry.Value)
		// attempt to ensure the hashmap doesn't share memory with our test oracle
		benchHashmap[string(keyBuf)] = string(valueBuf)
	}
}

func openTestFile(path string) (*bit.Table, *sparkey.HashReader, map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		_ = f.Close()
	}()

	bitDataFile, err := os.CreateTemp("", "bit-test.*.data")
	if err != nil {
		return nil, nil, nil, err
	}
	defer func() {
		_ = os.Remove(bitDataFile.Name())
		_ = os.Remove(bitDataFile.Name() + ".index")
	}()
	if err = bitDataFile.Close(); err != nil {
		return nil, nil, nil, err
	}
	if err = os.Remove(bitDataFile.Name()); err != nil {
		return nil, nil, nil, err
	}

	builder, err := bit.NewBuilder(bitDataFile.Name())
	if err != nil {
		return nil, nil, nil, err
	}

	sparkeyDataFile, err := os.CreateTemp("", "sparkey-test.*.data")
	if err != nil {
		return nil, nil, nil, err
	}
	_ = os.Remove(sparkeyDataFile.Name())
	_ = sparkeyDataFile.Close() // close it; we just want the name
	defer func() {
		_ = os.Remove(sparkeyDataFile.Name())
		_ = os.Remove(sparkeyDataFile.Name() + ".idx")
	}()

	sparkeyWriter, err := sparkey.CreateLogWriter(sparkeyDataFile.Name(), nil)

	known := make(map[string]string)

	s := bufio.NewScanner(bufio.NewReaderSize(f, 16*1024))
	for s.Scan() {
		line := s.Bytes()
		k, v, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			panic("input file unexpected shape")
		}
		err := builder.Put(k, v)
		if err != nil {
			return nil, nil, nil, err
		}
		err = sparkeyWriter.Put(k, v)
		if err != nil {
			return nil, nil, nil, err
		}
		known[string(k)] = string(v)
	}

	table, err := builder.Finalize()
	if err != nil {
		return nil, nil, nil, err
	}

	if err := sparkeyWriter.Flush(); err != nil {
		panic(err)
	}
	if err := sparkeyWriter.WriteHashFile(sparkey.HASH_SIZE_AUTO); err != nil {
		panic(err)
	}
	if err := sparkeyWriter.Close(); err != nil {
		panic(err)
	}

	sparkeyTable, err := sparkey.Open(sparkeyDataFile.Name())
	if err != nil {
		return nil, nil, nil, err
	}

	return table, sparkeyTable, known, nil
}

func BenchmarkBit(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % len(benchEntries)
		entry := benchEntries[j]
		value, ok := benchTableBit.GetString(entry.Key)
		if !ok || string(value) != entry.Value {
			b.Fatal("bad data or lookup")
		}
	}
}

func BenchmarkSparkey(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	iter, err := benchTableSparkey.Iterator()
	if err != nil {
		panic(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % len(benchEntries)
		entry := benchEntries[j]
		value, err := iter.Get(toBytes(entry.Key))
		if err != nil || string(value) != entry.Value {
			b.Fatal("bad data or lookup")
		}
	}
}

func BenchmarkHashmap(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % len(benchEntries)
		entry := benchEntries[j]
		value, ok := benchHashmap[entry.Key]
		if !ok || value != entry.Value {
			b.Fatal("bad data or lookup")
		}
	}
}

// toBytes returns a byte slice aliasing to the contents of the input string.
// Many hash functions are written to take []byte as input -- this lets us
// provide an API that takes a string and use those hash functions without a
// temporary allocation (and the garbage and copying string contents an allocation
// implies).
//
// SAFETY: the returned byte slice MUST NOT be written to, only read.
func toBytes(s string) (b []byte) {
	bh := (*reflect.SliceHeader)(unsafe.Pointer(&b))
	sh := *(*reflect.StringHeader)(unsafe.Pointer(&s))
	bh.Data = sh.Data
	bh.Len = sh.Len
	bh.Cap = sh.Len
	return b
}
