
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

// Code here is organized as follows:
// Exported methods are not called internally. They are just facades.
//   Marshal calls Encode 
//   Encode calls EncodeValue 
//   EncodeValue calls encodeValue 
// encodeValue and all other unexported functions use panics (not errors)
//    and may call other unexported functions (which use panics).
// 

import (
	"io"
	"bytes"
	"reflect"
	"math"
	"fmt"
	"time"
	"encoding/binary"
)

const (
	SEC  TimeResolution = iota // seconds
	MSEC                       // milliseconds (seconds / 1 000)
	USEC                       // microseconds (seconds / 1 000 000)
	NSEC                       // nanoseconds  (seconds / 1 000 000 000)
)

// Some tagging information for error messages.
var (
	msgTagEnc = "msgpack.encoder"
)

// TimeResolution describes the resolution with which to encode/decode time.
type TimeResolution int

// An Encoder writes an object to an output stream in the msgpack format.
type Encoder struct {
	w io.Writer
	opt EncoderOptions
	t []byte        //temp byte array re-used internally for efficiency
}

type EncoderOptions struct {
	// when encoding a time to Unix, what resolution do we use.
	TimeResolution TimeResolution
}

// NewDecoder returns an Encoder for encoding an object.
// If nil options are passed, we use a Encoder with options:
//    option: TimeResolution:          USEC
func NewEncoder(w io.Writer, opts *EncoderOptions) (*Encoder) {	
	if opts == nil {
		opts = &EncoderOptions{USEC}
	} 
	return &Encoder{w, *opts, make([]byte, 0, 16)}
}

// Encode writes an object into a stream in the MsgPack format.
// 
// time.Time is handled transparently.  We will encode to and from a int64 Unix time 
// (ie number of nanoseconds since Jan 1, 1970 Epoch) based on the resolution in the 
// EncoderOptions.
// 
// Struct values encode as maps. Each exported struct field is encoded
// unless:
//    - the field is Anonymous (embeded) (Symetry with Json,Gob)
//    - the field's tag is "-", or
//    - the field is empty and its tag specifies the "omitempty" option.
//
// The empty values are false, 0, any nil pointer or interface value, 
// and any array, slice, map, or string of length zero. 
// 
// The object's default key string is the struct field name but can be 
// specified in the struct field's tag value. 
// The "msgpack" key in struct field's tag value is the key name, 
// followed by an optional comma and options. 
// 
// To set an option on all fields (e.g. omitempty on all fields), you 
// can create a field called _struct, and set flags on it.
// 
// Examples:
//    
//      type MyStruct struct {
//          _struct bool    `msgpack:",omitempty"`   //set omitempty for every field
//          Field1 string   `msgpack:"-"`            //skip this field
//          Field2 int      `msgpack:"myName"`       //Use key "myName" in encode stream
//          Field3 int32    `msgpack:",omitempty"`   //use key "Field3". Omit if empty.
//          Field4 bool     `msgpack:"f4,omitempty"` //use key "f4". Omit if empty.
//          ...
//      }
//    
func (e *Encoder) Encode(v interface{}) (err error) {
	return e.EncodeValue(reflectValue(v))
}

// EncodeValue encodes a reflect.Value.
func (e *Encoder) EncodeValue(rv reflect.Value) (err error) {
	defer panicToErr(&err) 
	e.encodeValue(rv)
	return
}

func (e *Encoder) encode(v interface{}) {
	e.encodeValue(reflectValue(v))
}

func (e *Encoder) encodeValue(rv reflect.Value) {
	if !rv.IsValid() {
		e.encNil()
		return
	}
	
	// Tested with a type assertion for all common types first, but this increased encoding time
	// sometimes by up to 20% (weird). So just use the reflect.Kind switch alone.
	
	switch rk := rv.Kind(); rk {
	case reflect.Slice, reflect.Array:
		if rv.IsNil() {
			e.encNil()
			break
		}
		rawbytes := false
		if rv.Type() == byteSliceTyp {
			rawbytes = true
		}
		l := rv.Len()
		e.writeContainerLen(rawbytes, true, l)
		if rawbytes {
			e.writeb(l, rv.Bytes())
			break
		} 
		for j := 0; j < l; j++ {
			e.encode(rv.Index(j))
		}
	case reflect.Map:
		if rv.IsNil() {
			e.encNil()
			break
		}
		e.writeContainerLen(false, false, rv.Len())
		for _, mk := range rv.MapKeys() {
			e.encode(mk)
			e.encode(rv.MapIndex(mk))
		}
	case reflect.Struct:
		rt := rv.Type()
		//treat time.Time specially
		if rt == timeTyp {
			tnano := rv.Interface().(time.Time).UnixNano()
			e.encTime(tnano)
			break
		}
		sis := getStructFieldInfos(rt)
		encNames := make([]string, len(sis.sis))
		rvals := make([]reflect.Value, len(sis.sis))
		newlen := 0
		for _, si := range sis.sis {
			rval0 := rv.Field(si.i)
			if si.omitEmpty && isEmptyValue(rval0) {
				continue
			}
			encNames[newlen] = si.encName
			rvals[newlen] = rval0
			newlen++
		}
		
		e.writeContainerLen(false, false, newlen)
		for j := 0; j < newlen; j++ {
			e.encode([]byte(encNames[j]))
			e.encode(rvals[j])
		}
	case reflect.Bool:
		e.encBool(rv.Bool())
	case reflect.Float32:
		e.encFloat(true, uint64(math.Float32bits(float32(rv.Float()))))
	case reflect.Float64:
		e.encFloat(false, math.Float64bits(rv.Float()))
	case reflect.String:
		e.encode([]byte(rv.String()))
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		e.encInt(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		e.encUint(rv.Uint())
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			e.encNil()
			break
		}
		e.encodeValue(rv.Elem())
	default:
		panic(fmt.Errorf("%s: Unsupported kind: %s, for: %#v", msgTagEnc, rk, rv))
	}
	return
}

func (e *Encoder) writeContainerLen(rawbytes bool, slicelike bool, l int) {
	var b0, b1, b2 byte = 0x80, 0xde, 0xdf 
	var l0cutoff int = 16
	if rawbytes {
		l0cutoff = 32
		b0, b1, b2 = 0xa0, 0xda, 0xdb
	} else if slicelike { 
		b0, b1, b2 = 0x90, 0xdc, 0xdd
	}
	switch {
	case l < l0cutoff:
		e.t = e.t[0:1]
		e.t[0] = (b0 | byte(l))
	case l < 65536:
		e.t = e.t[0:3]
		e.t[0] = b1
		binary.BigEndian.PutUint16(e.t[1:], uint16(l))
	default:
		e.t = e.t[0:5]
		e.t[0] = b2
		binary.BigEndian.PutUint32(e.t[1:], uint32(l))
	}
	e.write()
}

func (e *Encoder) encNil() {
	e.t = e.t[0:1]
	e.t[0] = 0xc0
	e.write()
}

func (e *Encoder) encBool(b bool) {	
	e.t = e.t[0:1]
	if b {
		e.t[0] = 0xc3
	} else {
		e.t[0] = 0xc2
	}
	e.write()
}

func (e *Encoder) encInt(i int64) {
	switch {
	case i < math.MinInt32 || i > math.MaxInt32:
		e.t = e.t[0:9]
		e.t[0] = 0xd3
		binary.BigEndian.PutUint64(e.t[1:], uint64(i))
	case i < math.MinInt16 || i > math.MaxInt16:
		e.t = e.t[0:5]
		e.t[0] = 0xd2
		binary.BigEndian.PutUint32(e.t[1:], uint32(i))
	case i < math.MinInt8 || i > math.MaxInt8:
		e.t = e.t[0:3]
		e.t[0] = 0xd1
		binary.BigEndian.PutUint16(e.t[1:], uint16(i))
	case i < -32:
		e.t = e.t[0:2]
		e.t[0], e.t[1] = 0xd0, byte(i)
	case i >= -32 && i <= math.MaxInt8:
		e.t = e.t[0:1]
		e.t[0] = byte(i)
	default:
		panic("encInt64: Unreachable block")
	}
	e.write()
}

func (e *Encoder) encUint(i uint64) {
	e.encUintFl(false, false, i)
}

func (e *Encoder) encFloat(f32 bool, i uint64) {
	e.encUintFl(f32, !f32, i)
}

func (e *Encoder) encUintFl(f32 bool, f64 bool, i uint64) {
	switch {
	case f32:
		e.t = e.t[0:5]
		e.t[0] = 0xca
		binary.BigEndian.PutUint32(e.t[1:], uint32(i))
	case f64:
		e.t = e.t[0:9]
		e.t[0] = 0xcb
		binary.BigEndian.PutUint64(e.t[1:], i)
	case i <= math.MaxInt8:
		e.t = e.t[0:1]
		e.t[0] = byte(i)
	case i <= math.MaxUint8:
		e.t = e.t[0:2]
		e.t[0], e.t[1] = 0xcc, byte(i)
	case i <= math.MaxUint16:
		e.t = e.t[0:3]
		e.t[0] = 0xcd
		binary.BigEndian.PutUint16(e.t[1:], uint16(i))
	case i <= math.MaxUint32:
		e.t = e.t[0:5]
		e.t[0] = 0xce
		binary.BigEndian.PutUint32(e.t[1:], uint32(i))
	default:
		e.t = e.t[0:9]
		e.t[0] = 0xcf
		binary.BigEndian.PutUint64(e.t[1:], i)
	}
	e.write()
	return
}

func (e *Encoder) encTime(tnano int64) {
	//e.encode(tnano / 1e3); return
	//TODO: Perf: This switch statement increases encode time by 30% (169us to 242us)
	//when in calling function. Moving to its own functions to fix.
	switch e.opt.TimeResolution {
	case SEC:  
		tnano = tnano / 1e9
	case MSEC: 
		tnano = tnano / 1e6
	case USEC: 
		tnano = tnano / 1e3
	case NSEC: 
	default: 
		panic(fmt.Errorf("%s: Invalid Time Resolution: %d", msgTagEnc, e.opt.TimeResolution))
	}
	e.encode(tnano)
}

func (e *Encoder) write() {
	e.writeb(len(e.t), e.t)
}

func (e *Encoder) writeb(numbytes int, bs []byte) []byte {
	n, err := e.w.Write(bs)
	if err != nil {
		panic(err)
	}
	if n != numbytes {
		panic(fmt.Errorf("%s: write: Incorrect num bytes read. Expecting: %v, Wrote: %v", 
			msgTagEnc, numbytes, n))
	}
	return bs	
}

// Marshal is a convenience function which encodes v to a stream of bytes. 
// It delegates to Encoder.Encode.
func Marshal(v interface{}, opts *EncoderOptions) (b []byte, err error) {
	bs := new(bytes.Buffer)
	if err = NewEncoder(bs, opts).Encode(v); err == nil {
		b = bs.Bytes()
	}
	return
}


