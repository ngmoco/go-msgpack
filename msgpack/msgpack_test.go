
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
// If nil interface{}, then we have a different set of values to 
// verify against, understanding that decoding into a nil interface
// for example only uses 64-bit primitives (int64, uint64, float64),
// and []byte for primitive values, etc.
// 
// There are basically 2 tests here.
// First test internally encodes and decodes things and verifies that
// the artifact was as expected.
// Second test will use python msgpack to create a bunch of golden files,
// read those files, and compare them to what it should be. It then 
// writes those files back out and compares the byte streams.
//
// Altogether, the tests are pretty extensive.



import (
	"reflect"
	"testing"
	"fmt"
	"net/rpc"
	"bytes"
	"time"
	"os"
	"os/exec"
	"io/ioutil"
	"path/filepath"
	"strconv"
)

var (
	testLogToT = true
	skipVerifyVal interface{} = &(struct{}{})
	table []interface{}               // main items we encode
	tableVerify []interface{}         // we verify encoded things against this after decode
	tableTestNilVerify []interface{}  // for nil interface, use this to verify (rules are different)
)

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
	
	M map[interface{}]interface{}
	Msi64 map[string]int64
	
	N interface{} //don't set this, so we can test for nil
	T time.Time
}

func init() {
	_, _ = fmt.Printf, os.Remove

	// Note that if a map key type is interface{}, then it will be decoded to a nil interface{}
	// so we have to expect max-size objects (e.g. int64, uint64, etc).
	table = []interface{}{
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
		time.Date(2012, 2, 2, 2, 2, 2, 2, time.UTC),
		"someday",
		"",
		[]byte("bytestring"),
		[]interface{}{
			int64(-8),
			int64(-1616),
			int64(-32323232),
			int64(-6464646464646464),
			int64(8),
			uint64(1616),
			uint64(32323232),
			uint64(6464646464646464),
			int64(8),
			float64(-3232.0),
			float64(-6464646464.0),
			float64(3232.0),
			float64(6464646464.0),
			false,
			true,
			nil,
			"2012-02-02T02:02:02.000000002Z",
			"someday",
			"",
			"bytestring",
		},
		map[string]bool{
			"true":true,
			"false":false,
		},
		map[string]interface{}{
			"true": "True",
			"false": false,
			"int64(0)": int64(0),
		},
		//add a complex combo map in here. (map has list which has map)
		//note that after the first thing, everything else should be generic.
		map[string]interface{}{
			"list": []interface{}{
				int64(64),
				int64(32),
				true,
				float64(-32.0),
				map[string]interface{} {
					"TRUE":true,
					"FALSE":false,
				},
				[]interface{}{true, false},
			},
			"int32": int64(32),
			"bool": true,
			"LONG STRING": "123456789012345678901234567890123456789012345678901234567890",
			"SHORT STRING": "1234567890",
		},
		map[interface{}]interface{}{
			true: "true",
			int64(9): false,
			"false": int64(0),
		},
		newTestStruc(),
	}
	
	// we verify against the same table, but skip 23 
	// because interface{} equality is not defined exact for exact objects or nil.
	tableVerify = make([]interface{}, len(table))
	copy(tableVerify, table)
	tableVerify[23] = skipVerifyVal
	
	// For items decoded to a nil interface, the rules are different 
	// e.g. all primitives are 64-bit long (int64, uint64, float64, etc)
	// and all items are map[interface{}]interface{} except the required
	// bit mask is set.
	tableTestNilVerify = []interface{}{
		int64(-8),
		int64(-1616),
		int64(-32323232),
		int64(-6464646464646464),
		int64(8),
		uint64(1616),
		uint64(32323232),
		uint64(6464646464646464),
		int64(8),
		float64(-3232.0),
		float64(-6464646464.0),
		float64(3232.0),
		float64(6464646464.0),
		false,
		true,
		nil,
		"2012-02-02T02:02:02.000000002Z",
		"someday",
		"",
		"bytestring",
		[]interface{}{
			int64(-8),
			int64(-1616),
			int64(-32323232),
			int64(-6464646464646464),
			int64(8),
			uint64(1616),
			uint64(32323232),
			uint64(6464646464646464),
			int64(8),
			float64(-3232.0),
			float64(-6464646464.0),
			float64(3232.0),
			float64(6464646464.0),
			false,
			true,
			nil,
			"2012-02-02T02:02:02.000000002Z",
			"someday",
			"",
			"bytestring",
		},
		map[string]interface{}{
			"true":true,
			"false":false,
		},
		map[string]interface{}{
			"true": "True",
			"false": false,
			"int64(0)": int64(0),
		},
		map[string]interface{}{
			"list": []interface{}{
				int64(64),
				int64(32),
				true,
				float64(-32.0),
				map[string]interface{} {
					"TRUE":true,
					"FALSE":false,
				},
				[]interface{}{true, false},
			},
			"int32": int64(32),
			"bool": true,
			"LONG STRING": "123456789012345678901234567890123456789012345678901234567890",
			"SHORT STRING": "1234567890",
		},
		skipVerifyVal, //map[intf]intf cannot be compared for equality (equal undefined on key)
		skipVerifyVal,
	}
	
}

func lf(t *testing.T, format string, args ...interface{}) {
	if testLogToT {
		t.Logf(format, args...)		
	} else {
		if format[len(format)-1] != '\n' {
			format = format + "\n"
		}
		fmt.Printf(format, args...)
	}
}

func newTestStruc() TestStruc {
	//use uint64, int64, float64 when doing interface{} values, as those are the ones converted into 
	ts := TestStruc {
		S: "some string",
		I64: 64,
		I16: 16,
		Ui64: 64,
		Ui8: 8,
		B: true,
		By: 5,
		
		Sslice: []string{"one", "two", "three"},
		I64slice: []int64{1, 2, 3},
		I16slice: []int16{4, 5, 6},
		Ui64slice: []uint64{7, 8, 9},
		Ui8slice: []uint8{10, 11, 12},
		Bslice: []bool{true, false, true, false},
		Byslice: []byte{13, 14, 15},
		
		Islice: []interface{}{"true", true, "no", false, int64(0), float64(0.4)},
		
		M: map[interface{}]interface{}{
			true: "true",
			int64(9): false,
		},
		
		Msi64: map[string]int64{
			"one": 1,
			"two": 2,
		},
		T: time.Date(2012, 2, 2, 2, 2, 2, 2, time.UTC),
	}
	return ts
}

// doTestMsgpacks allows us test for different variations based on arguments passed.
func doTestMsgpacks(t *testing.T, testNil bool, decodeMask int, 
	vs []interface{}, vsVerify []interface{}) {
	//if testNil, then just test for when a pointer to a nil interface{} is passed. It should work.
	//Current setup allows us test (at least manually) the nil interface or typed interface.
	lf(t, "$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$*$\n")
	lf(t, "================ TestNil: %v ================\n", testNil)
	for i, v0 := range vs {
		lf(t, "..............................................")
		lf(t, "         Testing: #%d: %T, %#v\n", i, v0, v0)
		b0, err := Marshal(v0)
		if err != nil {
			lf(t, err.Error())
			t.Fail()
			continue
		}
		lf(t, "         Encoded bytes: len: %v, %v\n", len(b0), b0)
		
		var v1 interface{}
		
		dec := NewDecoder(bytes.NewBuffer(b0))
		if decodeMask > 0 {
			dec.OptionsMask = decodeMask
		}
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
			lf(t, "         v1 returned: %T, %#v", v1, v1)
			//we always indirect, because ptr to typed value may be passed (if not testNil)
			v1 = reflect.Indirect(reflect.ValueOf(v1)).Interface()
		}
		//v1 = indirIntf(v1, nil, -1)
		if err != nil {
			lf(t, "-------- Error: %v. Partial return: %v", err, v1)
			t.Fail()
			continue
		}
		v0check := vsVerify[i]
		if v0check == skipVerifyVal { 
			lf(t, "        Nil Check skipped: Decoded: %T, %#v\n", v1, v1)
			continue 
		}
		
		if reflect.DeepEqual(v0check, v1) { 
			lf(t, "++++++++ Before and After marshal matched\n")
		} else {
			lf(t, "-------- Before and After marshal do not match: " + 
				"(%T). Decoded: %T, %#v\n", v0, v1, v1)
			t.Fail()
		}
	}
}

func TestMsgpacks(t *testing.T) {	
	doTestMsgpacks(t, false, MASK_B2S_SLICE_ELEMENT | MASK_B2S_MAP_VALUE, table, tableVerify) 
	doTestMsgpacks(t, true, -1, table[:24], tableTestNilVerify[:24]) 
	doTestMsgpacks(t, true, MASK_B2S_SLICE_ELEMENT | MASK_B2S_MAP_VALUE, table[24:], 
		tableTestNilVerify[24:]) 
}

func TestDecodeToTypedNil(t *testing.T) {
	b, err := Marshal(32)
	var i *int32
	if err = Unmarshal(b, i); err == nil {
		lf(t, "------- Expecting error because we cannot unmarshal to int32 nil ptr")
		t.FailNow()
	}
	var i2 int32 = 0
	if err = Unmarshal(b, &i2); err != nil {
		lf(t, "------- Cannot unmarshal to int32 ptr. Error: %v", err)
		t.FailNow()
	}
	if i2 != int32(32) {
		lf(t, "------- didn't unmarshal to 32: Received: %d", *i)
		t.FailNow()
	}
}

// Test that we honor the rpc.ClientCodec and rpc.ServerCodec
func TestRpcInterface(t *testing.T) {
	c := new(rpcCodec)
	_ = func() { c.Close() }
	//f, _ := os.Open("some-random-file-jkjkjfkldlsfalkdljflsjljad")
	//c = NewRPCCodec(f)
	//c.Close()
	var _ rpc.ClientCodec = c
	var _ rpc.ServerCodec = c
}

// Comprehensive testing that generates data encoded from python msgpack, 
// and validates that our code can read and write it out accordingly.
func TestPythonGenStreams(t *testing.T) {
	lf(t, "TestPythonGenStreams")
	tmpdir, err := ioutil.TempDir("", "golang-msgpack-test") 
	if err != nil {
		lf(t, "-------- Unable to create temp directory\n")
		t.FailNow()
	}
	defer os.RemoveAll(tmpdir)
	lf(t, "tmpdir: %v", tmpdir)
	cmd := exec.Command("python", "build.py", "testdata", tmpdir)
	//cmd.Stdin = strings.NewReader("some input")
	//cmd.Stdout = &out
	if err = cmd.Run(); err != nil {
		lf(t, "-------- Error running python build.py. Err: %v", err)
		t.FailNow()
	}
	
	bss := make([][]byte, len(tableTestNilVerify) - 2)
	for i, v := range tableTestNilVerify[0:len(tableTestNilVerify) - 2] {
		//load up the golden file based on number
		//decode it
		//compare to in-mem object
		//encode it again
		//compare to output stream
		lf(t, "..............................................")
		lf(t, "         Testing: #%d: %T, %#v\n", i, v, v) 
		bss[i], err = ioutil.ReadFile(filepath.Join(tmpdir, strconv.Itoa(i) + ".golden"))
		if err != nil {
			lf(t, "-------- Error reading golden file: %d. Err: %v", i, err)
			t.Fail()
			continue
		}
		dec := NewDecoder(bytes.NewBuffer(bss[i]))
		var v1 interface{}
		if err = dec.Decode(&v1); err != nil {
			lf(t, "-------- Error decoding stream: %d: Err: %v", i, err)
			t.Fail()
			continue
		}
		//no need to indirect, because we pass a nil ptr, so we already have the value 
		//if v1 != nil { v1 = reflect.Indirect(reflect.ValueOf(v1)).Interface() }
		if reflect.DeepEqual(v, v1) { 
			lf(t, "++++++++ Objects match")
		} else {
			lf(t, "-------- Objects do not match: Source: %T. Decoded: %T", v, v1)
			lf(t, "--------   AGAINST: %#v", v)
			lf(t, "--------   DECODED: %#v", v1)
			t.Fail()
		}
		bsb := new(bytes.Buffer)
		if err = NewEncoder(bsb).Encode(v1); err != nil {
			lf(t, "Error encoding to stream: %d: Err: %v", i, err)
			t.Fail()
			continue
		}
		if reflect.DeepEqual(bsb.Bytes(), bss[i]) { 
			lf(t, "++++++++ Bytes match")
		} else {
			lf(t, "???????? Bytes do not match")
			xs := "--------"
			if reflect.ValueOf(v).Kind() == reflect.Map {
				xs = "        "
				lf(t, "%s It's a map. Ok that they don't match (dependent on ordering).", xs)
			} else {
				lf(t, "%s It's not a map. They should match.", xs)
				t.Fail()
			}
			lf(t, "%s   FROM_FILE: %4d] %v", xs, len(bss[i]), bss[i])
			lf(t, "%s     ENCODED: %4d] %v", xs, len(bsb.Bytes()), bsb.Bytes())
		}
	}
	
}

