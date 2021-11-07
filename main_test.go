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
	"github.com/colinmarc/cdb"
)

var (
	benchTableOnce    sync.Once
	benchTableBit     *bit.Table
	benchTableSparkey *sparkey.HashReader
	benchTableCdb     *cdb.CDB
	benchHashmap      map[string]string
	benchEntries      []benchEntry
)

type benchEntry struct {
	Key   string
	Value string
}

func loadBenchTable() {
	testData := "testdata.large"
	benchTableBit = createBitTable(testData)
	benchTableSparkey = createSparkeyTable(testData)
	benchTableCdb = createCdbTable(testData)
	benchHashmap = createInMemoryTable(testData)
	benchEntries = createEntriesTable(testData)
}

func streamTestFile(path string, put func(key, value []byte)) {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = f.Close()
	}()

	s := bufio.NewScanner(bufio.NewReaderSize(f, 16*1024))
	for s.Scan() {
		line := s.Bytes()
		k, v, ok := bytes.Cut(line, []byte{':'})
		if !ok {
			panic("input file unexpected shape")
		}
		put(k, v)
	}
}

func createInMemoryTable(testDataPath string) map[string]string {
	data := make(map[string]string)

	streamTestFile(testDataPath, func(k, v []byte) {
		data[string(k)] = string(v)

	})

	return data
}

func createEntriesTable(testDataPath string) []benchEntry {
	data := make(map[string]string)

	streamTestFile(testDataPath, func(k, v []byte) {
		data[string(k)] = string(v)
	})

	// we build it this way to ensure the list of entries is randomized and _doesn't_
	// match the order we wrote entries to the log files for the tables.
	entries := make([]benchEntry, 0, len(data))
	for k, v := range data {
		entries = append(entries, benchEntry{Key: k, Value: v})
	}

	return entries
}

func createBitTable(testDataPath string) *bit.Table {
	tableFile, err := os.CreateTemp("", "bit-test.*.data")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = os.Remove(tableFile.Name())
		_ = os.Remove(tableFile.Name() + ".index")
	}()
	if err = tableFile.Close(); err != nil {
		panic(err)
	}
	if err = os.Remove(tableFile.Name()); err != nil {
		panic(err)
	}

	builder, err := bit.NewBuilder(tableFile.Name())
	if err != nil {
		panic(err)
	}

	streamTestFile(testDataPath, func(k, v []byte) {
		if err := builder.Put(k, v); err != nil {
			panic(err)
		}
	})

	table, err := builder.Finalize()
	if err != nil {
		panic(err)
	}

	return table
}

func createSparkeyTable(testDataPath string) *sparkey.HashReader {
	tableFile, err := os.CreateTemp("", "bit-test.*.data")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = os.Remove(tableFile.Name())
		_ = os.Remove(tableFile.Name() + ".index")
	}()
	if err = tableFile.Close(); err != nil {
		panic(err)
	}
	if err = os.Remove(tableFile.Name()); err != nil {
		panic(err)
	}

	builder, err := sparkey.CreateLogWriter(tableFile.Name(), nil)
	if err != nil {
		panic(err)
	}

	streamTestFile(testDataPath, func(k, v []byte) {
		if err := builder.Put(k, v); err != nil {
			panic(err)
		}
	})

	if err := builder.Flush(); err != nil {
		panic(err)
	}
	if err := builder.WriteHashFile(sparkey.HASH_SIZE_AUTO); err != nil {
		panic(err)
	}
	if err := builder.Close(); err != nil {
		panic(err)
	}

	table, err := sparkey.Open(tableFile.Name())
	if err != nil {
		panic(err)
	}

	return table
}

func createCdbTable(testDataPath string) *cdb.CDB {
	tableFile, err := os.CreateTemp("", "bit-test.*.data")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = os.Remove(tableFile.Name())
		_ = os.Remove(tableFile.Name() + ".index")
	}()
	if err = tableFile.Close(); err != nil {
		panic(err)
	}
	if err = os.Remove(tableFile.Name()); err != nil {
		panic(err)
	}

	builder, err := cdb.Create(tableFile.Name())
	if err != nil {
		panic(err)
	}

	streamTestFile(testDataPath, func(k, v []byte) {
		if err := builder.Put(k, v); err != nil {
			panic(err)
		}
	})

	table, err := builder.Freeze()
	if err != nil {
		panic(err)
	}

	return table
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

func BenchmarkCdb(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		j := i % len(benchEntries)
		entry := benchEntries[j]
		value, err := benchTableCdb.Get(toBytes(entry.Key))
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
