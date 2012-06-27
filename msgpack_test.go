
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

// Test works by using a slice of interfaces.
// It can test for encoding/decoding into/from a nil interface{}
// or passing the object to encode/decode into.
//
// There are basically 2 main tests here.
// First test internally encodes and decodes things and verifies that
// the artifact was as expected.
// Second test will use python msgpack to create a bunch of golden files,
// read those files, and compare them to what it should be. It then 
// writes those files back out and compares the byte streams.
//
// Taken together, the tests are pretty extensive.



import (
	"reflect"
	"testing"
	"net/rpc"
	"bytes"
	"time"
	"os"
	"os/exec"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"net"
)

var (
	skipVerifyVal interface{} = &(struct{}{})
	timeToCompare = time.Date(2012, 2, 2, 2, 2, 2, 2000, time.UTC) //time.Time{} //
	//"2012-02-02T02:02:02.000002000Z" //1328148122000002
	timeToCompareAs interface{} = timeToCompare.UnixNano() 
	table []interface{}               // main items we encode
	tableVerify []interface{}         // we verify encoded things against this after decode
	tableTestNilVerify []interface{}  // for nil interface, use this to verify (rules are different)
	tablePythonVerify []interface{}   // for verifying for python, since Python sometimes
                                      // will encode a float32 as float64, or large int as uint
	testRpcInt = new(TestRpcInt)
)

type AnonInTestStruc struct {
	AS string
	AI64 int64
	AI16 int16
	AUi64 uint64
	ASslice []string
	AI64slice []int64
}

type TestStruc struct {
	S string
	I64 int64
	I16 int16
	Ui64 uint64
	Ui8 uint8
	B bool
	By byte
	
	Sslice []string
	I64slice []int64
	I16slice []int16
	Ui64slice []uint64
	Ui8slice []uint8
	Bslice []bool
	Byslice []byte
	
	Islice []interface{}
	Iptrslice []*int64
	
	AnonInTestStruc
	
	//M map[interface{}]interface{}  `json:"-",bson:"-"`
	Ms map[string]interface{}
	Msi64 map[string]int64
	
	Nintf interface{}    //don't set this, so we can test for nil
	T time.Time          
	Nmap map[string]bool //don't set this, so we can test for nil
	Nslice []byte        //don't set this, so we can test for nil
	Nint64 *int64
	Nteststruc *TestStruc
}

type TestRpcInt struct {
	i int
}

func (r *TestRpcInt) Update(n int, res *int) error { r.i = n; *res = r.i; return nil }
func (r *TestRpcInt) Square(ignore int, res *int) error { *res = r.i * r.i; return nil }
func (r *TestRpcInt) Mult(n int, res *int) error { *res = r.i * n; return nil }

func init() {
	primitives := []interface{} {
		int8(-8),
		int16(-1616),
		int32(-32323232),
		int64(-6464646464646464),
		uint8(8),
		uint16(1616),
		uint32(32323232),
		uint64(6464646464646464),
		byte(8),
		float32(-3232.0),
		float64(-6464646464.0),
		float32(3232.0),
		float64(6464646464.0),
		false,
		true,
		nil,
		timeToCompare,
		"someday",
		"",
		"bytestring",
	}
	mapsAndStrucs := []interface{}{
		map[string]bool{
			"true":true,
			"false":false,
		},
		map[string]interface{}{
			"true": "True",
			"false": false,
			"int64(0)": int8(0),
		},
		//add a complex combo map in here. (map has list which has map)
		//note that after the first thing, everything else should be generic.
		map[string]interface{}{
			"list": []interface{}{
				int16(1616),
				int32(32323232),
				true,
				float32(-3232.0),
				map[string]interface{} {
					"TRUE":true,
					"FALSE":false,
				},
				[]interface{}{true, false},
			},
			"int32": int32(32323232),
			"bool": true,
			"LONG STRING": "123456789012345678901234567890123456789012345678901234567890",
			"SHORT STRING": "1234567890",
		},
		map[interface{}]interface{}{
			true: "true",
			int8(8): false,
			"false": int8(0),
		},
		newTestStruc(0, false),
	}
	
	table = []interface{}{}
	table = append(table, primitives...)    //0-19 are primitives
	table = append(table, primitives)       //20 is a list of primitives
	table = append(table, mapsAndStrucs...) //21-24 are maps. 25 is a *struct

	// we verify against the same table, but skip 23 
	// because interface{} equality is not defined exact for exact objects or nil.
	var a, b []interface{}
	var c map[string]interface{}
	a = make([]interface{}, len(table))
	copy(a, table)
	b = make([]interface{}, len(a[20].([]interface{})))
	copy(b, a[20].([]interface{}))
	a[20] = b
	b[0], b[4], b[8], b[16], b[19] = int8(-8), int8(8), int8(8), 
		[]interface {}{int32(1328148122), int16(2000)}, "bytestring"
	a[23] = skipVerifyVal 
	//a[25] = skipVerifyVal
	tableVerify = a
	
	//when decoding into nil, for testing, 
	//we treat each []byte as string, and uint < 127 are decoded as int8.
	a = make([]interface{}, len(tableVerify))
	copy(a, tableVerify)
	a[0], a[4], a[8], a[16], a[19] = int8(-8), int8(8), int8(8), 
		[]interface {}{int32(1328148122), int16(2000)}, "bytestring"
	a[21] = map[string]interface{}{"true":true, "false":false}
	a[23] = table[23]
	a[25] = skipVerifyVal
	tableTestNilVerify = a
	
	//python msgpack encodes large positive numbers as unsigned, and all floats as float64
	a = make([]interface{}, len(tableTestNilVerify)-2)
	copy(a, tableTestNilVerify)
	a[23] = table[23]
	a[9], a[11], a[16] = float64(-3232.0), float64(3232.0), uint64(1328148122000002)
	b = make([]interface{}, len(a[20].([]interface{})))
	copy(b, a[20].([]interface{}))
	a[20] = b
	b[9], b[11], b[16] = float64(-3232.0), float64(3232.0), uint64(1328148122000002)
	c = make(map[string]interface{})
	for k, v := range a[23].(map[string]interface{}) { 
		c[k] = v
	}
	a[23] = c
	c["int32"] = uint32(32323232)
	b = c["list"].([]interface{})
	b[0], b[1], b[3] = uint16(1616), uint32(32323232), float64(-3232.0)
	tablePythonVerify = a
}

func newTestStruc(depth int, bench bool) (ts TestStruc) {
	var i64a, i64b, i64c, i64d int64 = 64, 6464, 646464, 64646464
	
	ts = TestStruc {
		S: "some string",
		I64: 64,
		I16: 16,
		Ui64: 64,
		Ui8: 160,
		B: true,
		By: 5,
		
		Sslice: []string{"one", "two", "three"},
		I64slice: []int64{1, 2, 3},
		I16slice: []int16{4, 5, 6},
		Ui64slice: []uint64{7, 8, 9},
		Ui8slice: []uint8{10, 11, 12},
		Bslice: []bool{true, false, true, false},
		Byslice: []byte{13, 14, 15},
		
		Islice: []interface{}{"true", true, "no", false, int8(88), float64(0.4)},
		
		Ms: map[string]interface{}{
			"true": "true",
			"int64(9)": false,
		},
		Msi64: map[string]int64{
			"one": 1,
			"two": 2,
		},
		T: timeToCompare,
		AnonInTestStruc: AnonInTestStruc{
			AS: "A-String",
			AI64: 64,
			AI16: 16,
			AUi64: 64,
			ASslice: []string{"Aone", "Atwo", "Athree"},
			AI64slice: []int64{1, 2, 3},
		},
	}
	//For benchmarks, some things will not work.
	if !bench {
		//json and bson require string keys in maps
		//ts.M = map[interface{}]interface{}{
		//	true: "true",
		//	int8(9): false,
		//}
		//gob cannot encode nil in element in array (encodeArray: nil element)
		ts.Iptrslice = []*int64{nil, &i64a, nil, &i64b, nil, &i64c, nil, &i64d, nil}
	}
	if depth > 0 {
		depth--
		ts.Ms["TestStruc." + strconv.Itoa(depth)] = newTestStruc(depth, bench)
		ts.Islice = append(ts.Islice, newTestStruc(depth, bench))
	}
	return
}

// doTestMsgpacks allows us test for different variations based on arguments passed.
func doTestMsgpacks(t *testing.T, testNil bool, opts DecoderContainerResolver, // *DecoderOptions DecoderContainerResolver,	
	vs []interface{}, vsVerify []interface{}) {
	//if testNil, then just test for when a pointer to a nil interface{} is passed. It should work.
	//Current setup allows us test (at least manually) the nil interface or typed interface.
	logT(t, "================ TestNil: %v ================\n", testNil)
	for i, v0 := range vs {
		logT(t, "..............................................")
		logT(t, "         Testing: #%d: %T, %#v\n", i, v0, v0)
		b0, err := Marshal(v0)
		if err != nil {
			logT(t, err.Error())
			failT(t)
			continue
		}
		logT(t, "         Encoded bytes: len: %v, %v\n", len(b0), b0)
		
		var v1 interface{}
		
		dec := NewDecoder(bytes.NewBuffer(b0), opts)
		if testNil {
			err = dec.Decode(&v1)
		} else {
			v0rt := intfTyp
			if v0 != nil { 
				v0rt = reflect.TypeOf(v0) 
			}
			v1 = reflect.New(v0rt).Interface()
			err = dec.Decode(v1)
		}
		
		if v1 != nil {
			logT(t, "         v1 returned: %T, %#v", v1, v1)
			//we always indirect, because ptr to typed value may be passed (if not testNil)
			v1 = reflect.Indirect(reflect.ValueOf(v1)).Interface()
		}
		if err != nil {
			logT(t, "-------- Error: %v. Partial return: %v", err, v1)
			failT(t)
			continue
		}
		v0check := vsVerify[i]
		if v0check == skipVerifyVal { 
			logT(t, "        Nil Check skipped: Decoded: %T, %#v\n", v1, v1)
			continue 
		}
		
		if reflect.DeepEqual(v0check, v1) { 
			logT(t, "++++++++ Before and After marshal matched\n")
		} else {
			logT(t, "-------- Before and After marshal do not match: " + 
				"(%T). ====> AGAINST: %T, %#v, DECODED: %T, %#v\n", v0, v0check, v0check, v1, v1)
			failT(t)
		}
	}
}


func TestMsgpacks(t *testing.T) {	
	doTestMsgpacks(t, false, 
		testDecOpts(nil, nil, false, true, true), 
		table, tableVerify) 
}

func TestMsgpacksNilStringMap(t *testing.T) {	
	doTestMsgpacks(t, true,  
		testDecOpts(mapStringIntfTyp, nil, true, true, true), 
		table[:24], tableTestNilVerify[:24]) 
}

func TestMsgpacksNilIntf(t *testing.T) {	
	doTestMsgpacks(t, true, 
		testDecOpts(nil, nil, false, true, true), 
		table[24:], tableTestNilVerify[24:]) 
}

func TestDecodeToTypedNil(t *testing.T) {
	b, err := Marshal(32)
	var i *int32
	if err = Unmarshal(b, i, nil); err == nil {
		logT(t, "------- Expecting error because we cannot unmarshal to int32 nil ptr")
		t.FailNow()
	}
	var i2 int32 = 0
	if err = Unmarshal(b, &i2, nil); err != nil {
		logT(t, "------- Cannot unmarshal to int32 ptr. Error: %v", err)
		t.FailNow()
	}
	if i2 != int32(32) {
		logT(t, "------- didn't unmarshal to 32: Received: %d", *i)
		t.FailNow()
	}
}

func TestDecodePtr(t *testing.T) {
	ts := newTestStruc(0, false)
	b, err := Marshal(&ts)
	if err != nil {
		logT(t, "------- Cannot Marshal pointer to struct. Error: %v", err)
		t.FailNow()
	} else if len(b) < 40 {
		logT(t, "------- Size must be > 40. Size: %d", len(b))
		t.FailNow()
	}
	ts2 := new(TestStruc)
	err = Unmarshal(b, &ts2, nil)
	if err != nil {
		logT(t, "------- Cannot Unmarshal pointer to struct. Error: %v", err)
		t.FailNow()
	} else if ts2.I64 != 64 {
		logT(t, "------- Unmarshal wrong. Expect I64 = 64. Got: %v", ts2.I64)
		t.FailNow()
	}
}

func TestIntfDecode(t *testing.T) {
	m := map[string]int{"A":2, "B":3, }
	p := []interface{}{m}
	bs, err := Marshal(p)
	if err != nil {
		logT(t, "Error marshalling p: %v, Err: %v", p, err)
		t.FailNow()
	}
	m2 := map[string]int{}
	p2 := []interface{}{m2}
    err = Unmarshal(bs, &p2, nil)
	if err != nil {
		logT(t, "Error unmarshalling into &p2: %v, Err: %v", p2, err)
		t.FailNow()
	}
	
	if m2["A"] != 2 || m2["B"] != 3 {
		logT(t, "m2 not as expected: expecting: %v, got: %v", m, m2)
		t.FailNow()
	}
	// log("m: %v, m2: %v, p: %v, p2: %v", m, m2, p, p2)
	if reflect.DeepEqual(p, p2) {
		logT(t, "p and p2 match")
	} else {
		logT(t, "Not Equal: p: %v, p2: %v", p, p2)
		t.FailNow()
	}
	if reflect.DeepEqual(m, m2) {
		logT(t, "m and m2 match")
	} else {
		logT(t, "Not Equal: m: %v, m2: %v", m, m2)
		t.FailNow()
	}
}

func TestDecodeStructSubset(t *testing.T) {
	// test that we can decode a subset of the stream
	m := map[string]interface{}{"A": 5, "B": 99, "C": 333, }
	bs, err := Marshal(m)
	if err != nil {
		logT(t, "Error marshalling m: %v, Err: %v", m, err)
		t.FailNow()
	}
	type ttt struct {
		A uint8
		C int32
	}
	var t2 ttt
	err = Unmarshal(bs, &t2, nil)
	if err != nil {
		logT(t, "Error unmarshalling into &t2: %v, Err: %v", t2, err)
		t.FailNow()
	}
	t3 := ttt{5, 333}
	if !reflect.DeepEqual(t2, t3) {
		logT(t, "Not Equal: t2: %v, t3: %v", t2, t3)
		t.FailNow()
	}
}

// comment out for now
func TestRpcAll(t *testing.T) {
	testRpc(t, true, true, true, true)
}

func TestRpc(t *testing.T) {
	testRpc(t, true, true, false, true)
}

func TestCustomRpc(t *testing.T) {
	testRpc(t, true, false, true, true)
}

func testRpc(t *testing.T, callClose, doBasic, doCustom, doExit bool) {
	srv := rpc.NewServer()
	srv.Register(testRpcInt)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	// log("listener: %v", ln.Addr())
	checkErrT(t, err)
	defer ln.Close()
	
	var opts DecoderContainerResolver
	serverExitChan := make(chan bool, 1)
	serverFn := func() {
		for { 
			conn1, err1 := ln.Accept()
			if err1 != nil {
				continue
			}
			bs := make([]byte, 1)
			n1, err1 := conn1.Read(bs)
			if n1 != 1 || err1 != nil {
				conn1.Close()
				continue
			}
			var sc rpc.ServerCodec
			switch bs[0] {
			case 'B': 
				sc = NewRPCServerCodec(conn1, opts)
			case 'C':
				sc = NewCustomRPCServerCodec(conn1, opts)
			case 'X':
				serverExitChan <- true
				conn1.Close()
				return // exit serverFn goroutine
			}
			if sc == nil {
				conn1.Close()
				continue
			}
			srv.ServeCodec(sc)
			// for {
			// 	if err1 = srv.ServeRequest(sc); err1 != nil {
			// 		break
			// 	}
			// }
			// if callClose {
			// 	sc.Close() 
			// }
		}
	}
	
	clientFn := func(cc rpc.ClientCodec) {
		cl := rpc.NewClientWithCodec(cc)
		if callClose {
			defer cl.Close() 
		} 
		var up, sq, mult int
		// log("Calling client")
		checkErrT(t, cl.Call("TestRpcInt.Update", 5, &up))
		// log("Called TestRpcInt.Update")
		checkEqualT(t, testRpcInt.i, 5)
		checkEqualT(t, up, 5)
		checkErrT(t, cl.Call("TestRpcInt.Square", 1, &sq))
		checkEqualT(t, sq, 25)
		checkErrT(t, cl.Call("TestRpcInt.Mult", 20, &mult))
		checkEqualT(t, mult, 100)		
	}
	
	connFn := func(req byte) (bs net.Conn) {
		// log("calling f1")
		bs, err2 := net.Dial(ln.Addr().Network(), ln.Addr().String())
		// log("f1. bs: %v, err2: %v", bs, err2)
		checkErrT(t, err2)
		n1, err2 := bs.Write([]byte{req})
		checkErrT(t, err2)
		checkEqualT(t, n1, 1)
		return
	}
	
	go serverFn()
	if doBasic {
		bs := connFn('B')
		cc := NewRPCClientCodec(bs, opts)
		clientFn(cc)
	}
	if doCustom {
		bs := connFn('C')
		cc := NewCustomRPCClientCodec(bs, opts)
		clientFn(cc)
	}
	if doExit {
		bs := connFn('X')
		<- serverExitChan
		bs.Close()
	}
}

// Comprehensive testing that generates data encoded from python msgpack, 
// and validates that our code can read and write it out accordingly.
func TestPythonGenStreams(t *testing.T) {
	logT(t, "TestPythonGenStreams")
	tmpdir, err := ioutil.TempDir("", "golang-msgpack-test") 
	if err != nil {
		logT(t, "-------- Unable to create temp directory\n")
		t.FailNow()
	}
	defer os.RemoveAll(tmpdir)
	logT(t, "tmpdir: %v", tmpdir)
	cmd := exec.Command("python", "helper.py", "testdata", tmpdir)
	//cmd.Stdin = strings.NewReader("some input")
	//cmd.Stdout = &out
	var cmdout []byte
	if cmdout, err = cmd.CombinedOutput(); err != nil {
		logT(t, "-------- Error running python build.py. Err: %v", err)
		logT(t, "         %v", string(cmdout))
		t.FailNow()
	}
	
	for i, v := range tablePythonVerify {
		//load up the golden file based on number
		//decode it
		//compare to in-mem object
		//encode it again
		//compare to output stream
		logT(t, "..............................................")
		logT(t, "         Testing: #%d: %T, %#v\n", i, v, v) 
		var bss []byte
		bss, err = ioutil.ReadFile(filepath.Join(tmpdir, strconv.Itoa(i) + ".golden"))
		if err != nil {
			logT(t, "-------- Error reading golden file: %d. Err: %v", i, err)
			failT(t)
			continue
		}
		dec := NewDecoder(bytes.NewBuffer(bss),
			testDecOpts(mapStringIntfTyp, nil, true, true, true))
		var v1 interface{}
		if err = dec.Decode(&v1); err != nil {
			logT(t, "-------- Error decoding stream: %d: Err: %v", i, err)
			failT(t)
			continue
		}
		if v == skipVerifyVal {
			continue
		}
		//no need to indirect, because we pass a nil ptr, so we already have the value 
		//if v1 != nil { v1 = reflect.Indirect(reflect.ValueOf(v1)).Interface() }
		if reflect.DeepEqual(v, v1) { 
			logT(t, "++++++++ Objects match")
		} else {
			logT(t, "-------- Objects do not match: Source: %T. Decoded: %T", v, v1)
			logT(t, "--------   AGAINST: %#v", v)
			logT(t, "--------   DECODED: %#v", v1)
			failT(t)
		}
		bsb := new(bytes.Buffer)
		if err = NewEncoder(bsb).Encode(v1); err != nil {
			logT(t, "Error encoding to stream: %d: Err: %v", i, err)
			failT(t)
			continue
		}
		if reflect.DeepEqual(bsb.Bytes(), bss) { 
			logT(t, "++++++++ Bytes match")
		} else {
			logT(t, "???????? Bytes do not match")
			xs := "--------"
			if reflect.ValueOf(v).Kind() == reflect.Map {
				xs = "        "
				logT(t, "%s It's a map. Ok that they don't match (dependent on ordering).", xs)
			} else {
				logT(t, "%s It's not a map. They should match.", xs)
				failT(t)
			}
			logT(t, "%s   FROM_FILE: %4d] %v", xs, len(bss), bss)
			logT(t, "%s     ENCODED: %4d] %v", xs, len(bsb.Bytes()), bsb.Bytes())
		}
	}
	
}

func testDecOpts(MapType reflect.Type, SliceType reflect.Type, BytesStringLiteral bool,
	BytesStringSliceElement bool, BytesStringMapValue bool) DecoderContainerResolver {
	return &SimpleDecoderContainerResolver {
		MapType, SliceType, BytesStringLiteral, BytesStringSliceElement, BytesStringMapValue,
	}
}
