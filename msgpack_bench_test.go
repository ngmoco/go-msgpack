
/*
go-msgpack - Msgpack library for Go. Provides pack/unpack and net/rpc support.
https://github.com/ugorji/go-msgpack

Copyright (c) 2012, Ugorji Nwoke.
All rights reserved.

Redistribution and use in source and binary forms, with or without modification,
are permitted provided that the following conditions are met:

* Redistributions of source code must retain the above copyright notice,
  this list of conditions and the following disclaimer.
* Redistributions in binary form must reproduce the above copyright notice,
  this list of conditions and the following disclaimer in the documentation
  and/or other materials provided with the distribution.
* Neither the name of the author nor the names of its contributors may be used
  to endorse or promote products derived from this software
  without specific prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND
ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED
WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE
DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR
ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES
(INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES;
LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION) HOWEVER CAUSED AND ON
ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT LIABILITY, OR TORT
(INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE OF THIS
SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
*/

package msgpack

import (
	"launchpad.net/mgo/bson"
	"encoding/json"
	"encoding/gob"
	"testing"
	"bytes"
	"reflect"
	"time"
	"runtime"
	"flag"
)

var (
	//depth of 0 maps to ~400bytes json-encoded string, 1 maps to ~1400 bytes, etc
	benchDepth = 0
	benchBs []byte
	benchTs TestStruc

	benchDoInitBench = flag.Bool("mb", false, "Run Bench Init")
)

type benchFn func(buf *bytes.Buffer, ts *TestStruc) error

func init() {
	flag.Parse()
	gob.Register(new(TestStruc))
	benchTs = newTestStruc(benchDepth, true)
	approxSize := approxDataSize(reflect.ValueOf(benchTs))
	//benchBs = make([]byte, 1024 * 4 * d) 
	//initialize benchBs large enough to hold double approx size 
	//(to fit all encodings without expansion)
	benchBs = make([]byte, approxSize * 2) 
	if *benchDoInitBench {
		logT(nil, "")
		logT(nil, "..............................................")
		logT(nil, "BENCHMARK INIT: %v", time.Now())
		logT(nil, "TO RUN FULL BENCHMARK comparing MsgPack, JSON,BSON,GOB, " + 
			"use \"go test -test.bench .\"")
		logT(nil, "Benchmark: " + 
			"\n\tinit []byte size:                   %d, " +
			"\n\tStruct recursive Depth:             %d, " + 
			"\n\tApproxDeepSize Of benchmark Struct: %d, ", 
			len(benchBs), benchDepth, approxSize, )
		benchCheck()
		logT(nil, "..............................................")
	}	
}

func benchCheck() {
	fn := func(name string, encfn benchFn, decfn benchFn) {
		benchBs = benchBs[0:0]
		buf := bytes.NewBuffer(benchBs)
		var err error
		runtime.GC()
		tnow := time.Now()
		if err = encfn(buf, &benchTs); err != nil {
			logT(nil, "\t%10s: **** Error encoding benchTs: %v", name, err)
		} 
		encDur := time.Now().Sub(tnow)
		encLen := buf.Len()
		//log("\t%10s: encLen: %v, len: %v, cap: %v\n", name, encLen, len(benchBs), cap(benchBs))
		buf = bytes.NewBuffer(benchBs[0:encLen])
		runtime.GC()
		tnow = time.Now()
		if err = decfn(buf, new(TestStruc)); err != nil {
			logT(nil, "\t%10s: **** Error decoding into new TestStruc: %v", name, err)
		}
		decDur := time.Now().Sub(tnow)
		logT(nil, "\t%10s: Encode Size: %5d, Encode Time: %8v, Decode Time: %8v", name, encLen, encDur, decDur)
	}
	logT(nil, "Benchmark One-Pass Unscientific Marshal Sizes:")
	fn("msgpack", fnMsgpackEncodeFn, fnMsgpackDecodeFn)
	fn("gob", fnGobEncodeFn, fnGobDecodeFn)
	fn("bson", fnBsonEncodeFn, fnBsonDecodeFn)
	fn("json", fnJsonEncodeFn, fnJsonDecodeFn)
}

func fnBenchmarkEncode(b *testing.B, encfn benchFn) {
	//benchOnce.Do(benchInit)
	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchBs = benchBs[0:0]
		buf := bytes.NewBuffer(benchBs)
		if err := encfn(buf, &benchTs); err != nil {
			logT(b, "Error encoding benchTs: %v", err)
			b.FailNow()
		}
	}
}

func fnBenchmarkDecode(b *testing.B, encfn benchFn, decfn benchFn) {
	var err error
	benchBs = benchBs[0:0]
	buf := bytes.NewBuffer(benchBs)
	if err = encfn(buf, &benchTs); err != nil {
		logT(b, "Error encoding benchTs: %v", err)
		b.FailNow()
	}
	encLen := buf.Len()
	benchBs = benchBs[0:encLen]
	runtime.GC()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ts := new(TestStruc)		
		buf = bytes.NewBuffer(benchBs)
		if err = decfn(buf, ts); err != nil {
			logT(b, "Error decoding into new TestStruc: %v", err)
			b.FailNow()
		}
		if ts.I64slice[2] != int64(3) {
			logT(b, "Error: Decode failed by checking values")
			b.FailNow()
		}
	}
}

func fnMsgpackEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return NewEncoder(buf).Encode(ts)
}

func fnMsgpackDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	//return NewDecoder(buf, nil).Decode(ts)
	return NewDecoder(buf, testDecOpts(nil, nil, false, false, false)).Decode(ts)
}

func fnGobEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return gob.NewEncoder(buf).Encode(ts)
}

func fnGobDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return gob.NewDecoder(buf).Decode(ts)
}

func fnJsonEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return json.NewEncoder(buf).Encode(ts)
}

func fnJsonDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return json.NewDecoder(buf).Decode(ts)
}

func fnBsonEncodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	bs, err := bson.Marshal(ts)
	if err == nil {
		buf.Write(bs)
	}
	return err
}

func fnBsonDecodeFn(buf *bytes.Buffer, ts *TestStruc) error {
	return bson.Unmarshal(buf.Bytes(), ts)
}

func Benchmark__Msgpack__Encode(b *testing.B) {
	fnBenchmarkEncode(b, fnMsgpackEncodeFn)
}

func Benchmark__Gob______Encode(b *testing.B) {
	fnBenchmarkEncode(b, fnGobEncodeFn)
}

func Benchmark__Bson_____Encode(b *testing.B) {
	fnBenchmarkEncode(b, fnBsonEncodeFn)
}

func Benchmark__Json_____Encode(b *testing.B) {
	fnBenchmarkEncode(b, fnJsonEncodeFn)
}

func Benchmark__Msgpack__Decode(b *testing.B) {
	fnBenchmarkDecode(b, fnMsgpackEncodeFn, fnMsgpackDecodeFn)
}

func Benchmark__Gob______Decode(b *testing.B) {
	fnBenchmarkDecode(b, fnGobEncodeFn, fnGobDecodeFn)
}

func Benchmark__Bson_____Decode(b *testing.B) {
	fnBenchmarkDecode(b, fnBsonEncodeFn, fnBsonDecodeFn)
}

func Benchmark__Json_____Decode(b *testing.B) {
	fnBenchmarkDecode(b, fnJsonEncodeFn, fnJsonDecodeFn)
}

