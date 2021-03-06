// Copyright 2021 The bit Authors. All rights reserved.
// Use of this source code is governed by the MIT License
// that can be found in the LICENSE file.

package bitbenchmark

import (
	"bufio"
	"bytes"
	"math/rand"
	"os"
	"reflect"
	"sync"
	"testing"
	"unsafe"

	"github.com/bpowers/bit"
	"github.com/bsm/go-sparkey"
	"github.com/colinmarc/cdb"
)

const testData = "testdata.large"

var (
	benchTableOnce                sync.Once
	benchTableBit                 *bit.Table
	benchTableSparkeyUncompressed *sparkey.HashReader
	// benchTableSparkeySnappy       *sparkey.HashReader
	benchTableCdb *cdb.CDB
	benchHashmap  map[string]string
	benchEntries  []benchEntry
)

type benchEntry struct {
	Key   string
	Value string
}

func loadBenchTable() {
	benchTableBit = createBitTable(testData)
	benchTableSparkeyUncompressed = createSparkeyTable(testData, false)
	// benchTableSparkeySnappy = createSparkeyTable(testData, true)
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

func createSparkeyTable(testDataPath string, compressedWithSnappy bool) *sparkey.HashReader {
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

	var opts *sparkey.Options
	if compressedWithSnappy {
		opts.Compression = sparkey.COMPRESSION_SNAPPY
	}
	builder, err := sparkey.CreateLogWriter(tableFile.Name(), opts)
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

func BenchmarkBitGet(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(b *testing.PB) {
		entryCount := len(benchEntries)
		i := rand.Int() % entryCount
		for b.Next() {
			entry := benchEntries[i]
			value, ok := benchTableBit.GetString(entry.Key)
			if !ok || string(value) != entry.Value {
				panic("bad data or lookup")
			}
			i = (i + 1) % entryCount
		}
	})
}

func BenchmarkMapGet(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(b *testing.PB) {
		entryCount := len(benchEntries)
		i := rand.Int() % entryCount
		for b.Next() {
			entry := benchEntries[i]
			value, ok := benchHashmap[entry.Key]
			if !ok || value != entry.Value {
				panic("bad data or lookup")
			}
			i = (i + 1) % entryCount
		}
	})
}

//func BenchmarkSparkeySnappyGet(b *testing.B) {
//	benchTableOnce.Do(loadBenchTable)
//
//	b.ReportAllocs()
//	b.ResetTimer()
//	b.RunParallel(func(b *testing.PB) {
//		iter, err := benchTableSparkeySnappy.Iterator()
//		if err != nil {
//			panic(err)
//		}
//
//		entryCount := len(benchEntries)
//		i := rand.Int() % entryCount
//		for b.Next() {
//			entry := benchEntries[i]
//			value, err := iter.Get(toBytes(entry.Key))
//			if err != nil || string(value) != entry.Value {
//				panic("bad data or lookup")
//			}
//
//			i = (i + 1) % entryCount
//		}
//	})
//}

func BenchmarkSparkeyUncompressedGet(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(b *testing.PB) {
		iter, err := benchTableSparkeyUncompressed.Iterator()
		if err != nil {
			panic(err)
		}

		entryCount := len(benchEntries)
		i := rand.Int() % entryCount
		for b.Next() {
			entry := benchEntries[i]
			value, err := iter.Get(toBytes(entry.Key))
			if err != nil || string(value) != entry.Value {
				panic("bad data or lookup")
			}

			i = (i + 1) % entryCount
		}
	})
}

func BenchmarkCdbGet(b *testing.B) {
	benchTableOnce.Do(loadBenchTable)

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(b *testing.PB) {
		entryCount := len(benchEntries)
		i := rand.Int() % entryCount
		for b.Next() {
			entry := benchEntries[i]
			value, err := benchTableCdb.Get(toBytes(entry.Key))
			if err != nil || string(value) != entry.Value {
				panic("bad data or lookup")
			}
			i = (i + 1) % entryCount
		}
	})
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

var (
	benchTableBitCreate     *bit.Table
	benchTableSparkeyCreate *sparkey.HashReader
	benchTableCdbCreate     *cdb.CDB
)

func BenchmarkBitCreate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchTableBitCreate = createBitTable(testData)
		if benchTableBitCreate == nil {
			b.Fatal("bad data or lookup")
		}
	}
}

func BenchmarkSparkeyCreateUncompressed(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchTableSparkeyCreate = createSparkeyTable(testData, false)
		if benchTableSparkeyCreate == nil {
			b.Fatal("bad data or lookup")
		}
	}
}

//func BenchmarkSparkeyCreateSnappy(b *testing.B) {
//	b.ReportAllocs()
//	b.ResetTimer()
//	for i := 0; i < b.N; i++ {
//		benchTableSparkeyCreate = createSparkeyTable(testData, true)
//		if benchTableSparkeyCreate == nil {
//			b.Fatal("bad data or lookup")
//		}
//	}
//}

func BenchmarkCdbCreate(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchTableCdbCreate = createCdbTable(testData)
		if benchTableCdbCreate == nil {
			b.Fatal("bad data or lookup")
		}
	}
}

func TestBitTableCreate(t *testing.T) {
	table := createBitTable(testData)
	if table == nil {
		t.Fatal("expected table to be non-nil")
	}
}
