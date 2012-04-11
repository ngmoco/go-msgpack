
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
//   Unmarshal calls Decode 
//   Decode calls DecodeValue 
//   DecodeValue calls decodeValue 
// decodeValue and all other unexported functions use panics (not errors)
//    and may call other unexported functions (which use panics).

import (
	"io"
	"bytes"
	"reflect"
	"math"
	"fmt"
	"time"
	"encoding/binary"
)

// These Masks determine how to decode into a nil interface{}.
// These masks are all important for supporting JSON compatibility
// when decoding into a nil interface{}.
// See NewDecoder() for more information.
const (
	// If set and we detect raw bytes while decoding into a interface{},
	// convert the raw bytes to a string. 
	MASK_B2S_LITERAL        = 1 << iota
	
	// If set and we detect raw bytes while decoding into a []interface{},
	// convert the raw bytes to a string. 
	MASK_B2S_SLICE_ELEMENT 
	
	// If set and we detect raw bytes while decoding into a value in a map[XXX]interface{},
	// convert the raw bytes to a string. 
	MASK_B2S_MAP_VALUE
	
	// If set and we detect a Map from the stream, we initialize a map[string]interface{}.
	// If not set, then a map[interface{}]interface{} is created.
	MASK_MAP_KEYS_STRING
)

// Some tagging information for error messages.
var (
	msgTagDec = "msgpack.decoder"
	msgBadDesc = "Unrecognized descriptor byte: "
)

// An Decoder reads and decodes an object from an input stream in the msgpack format.
type Decoder struct {
	t []byte        //temp byte array re-used internally for efficiency
	r io.Reader     
	OptionsMask int //options
}

// NewDecoder returns a Decoder for decoding a stream of bytes into an object.
// OptionsMask defaults to "all set", for full JSON compatibility i.e.
//    MASK_MAP_KEYS_STRING | MASK_B2S_LITERAL | MASK_B2S_SLICE_ELEMENT | MASK_B2S_MAP_VALUE
// 
// Consequently, we default to decoding with JSON compatibility
// (there are no []byte, only strings. And all keys in maps are strings.)
//
// You can change the options if you want, before calling Decode.
func NewDecoder(r io.Reader) (*Decoder) {
	return &Decoder{
		r: r,
		t: make([]byte, 0, 16),
		OptionsMask: MASK_MAP_KEYS_STRING | MASK_B2S_LITERAL | 
			MASK_B2S_SLICE_ELEMENT | MASK_B2S_MAP_VALUE,
	}
}

// Decode decodes the stream from reader and stores the result in the 
// value pointed to by v.
// 
// If v is a pointer to a non-nil value, we will decode the stream into that value 
// (if the value type and the stream match. For example:
// integer in stream must go into int type (int8...int64), etc
// 
// If you do not know what type of stream it is, pass in a pointer to a nil interface.
// We will decode and store a value in that nil interface. 
// 
// When decoding into a nil interface{}, the following rules apply as we have 
// to make assumptions about the specific types you want.
//    - Only max-size primitives are used (int64, uint64, float64)
//    - Maps are decoded as map[interface{}]interface{} or map[string]interface{}
//      depending on if MASK_B2S_MAP_VALUE is set in OptionsMask
//    - raw bytes are decoded into []byte or string depending on OptionsMask setting of:
//      MASK_B2S_SLICE_ELEMENT, MASK_B2S_LITERAL, MASK_MAP_KEYS_STRING
//    - Lists are always decoded as []interface{}
//    - We still decode integers, floats, bool, Null, into these containers out of the stream.
// 
// time.Time is handled transparently. We can decode an RFC3339 encoded string
// into a time.Time struct or pointer.
// 
// Sample usages:
//   // Decoding into a non-nil typed value
//   var f float32
//   err = msgpack.NewDecoder(r).Decode(&f)
//
//   // Decoding into nil interface
//   var v interface{}
//   dec := msgpack.NewDecoder(r)
//   // If you don't want all masks set, reset OptionsMask on Decoder (see Mask docs)
//   dec.OptionsMask = msgpack.MASK_B2S_SLICE_ELEMENT | msgpack.MASK_B2S_MAP_VALUE
//   err = dec.Decode(&v) 
//   
func (d *Decoder) Decode(v interface{}) (err error) {
	return d.DecodeValue(reflectValue(v))
}

// DecodeValue decodes the stream into a reflect.Value.
// The reflect.Value must be a pointer.
// See Decoder.Decode documentation. (Decode internally calls DecodeValue).
func (d *Decoder) DecodeValue(rv reflect.Value) (err error) {
	defer panicToErr(&err)
	// We cannot marshal into a non-pointer or a nil pointer 
	// (at least pass a nil interface so we can marshal into it)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		err = fmt.Errorf("%v: DecodeValue: Expecting a valid pointer to decode into. Got: %v, %T, %v", 
			msgTagDec, rv.Kind(), rv.Interface(), rv.Interface())
		return
	}

	//if a nil pointer is passed, set rv to the underlying value (not pointer).
	rve := rv.Elem()
	wasNilIntf := rve.Interface() == nil
	d.decodeValue(0, true, rv)
	if wasNilIntf && rve.Interface() != nil {
		rve.Set(rve.Elem().Elem())
	}
	return
}

func (d *Decoder) bytesAsStringFn(slsize int) (rvn reflect.Value) {
	if isMask(d.OptionsMask, MASK_B2S_LITERAL) {
		rvm := ""
		rvn = reflect.ValueOf(&rvm)
	} else {
		rvm := make([]byte, 0, slsize)
		rvn = reflect.ValueOf(&rvm)
	}
	return
}

func (d *Decoder) makeMapFn(msize int) (rvn reflect.Value) {
	if isMask(d.OptionsMask, MASK_MAP_KEYS_STRING) {
		rvm := make(map[string]interface{}, msize)
		rvn = reflect.ValueOf(&rvm)
	} else {
		rvm := make(map[interface{}]interface{}, msize)
		rvn = reflect.ValueOf(&rvm)
	}
	return
}

func (d *Decoder) decodeValue(bd byte, readDesc bool, rv reflect.Value) {
	if readDesc {
		d.read(1)
		bd = d.t[0]
	}

	rvi := rv.Interface()
	//if nil, use some hieristics to set the nil interface to an 
	//appropriate value based on the first byte read (byte descriptor bd)
	if rvi == nil {
		var rvn reflect.Value		
		//The order is very critical. E.g. we have to check for exact 
		//container matches before checking for shared length matches.
		switch {
		case int8(bd) > -32 || bd <= math.MaxInt8:
			//(Fixnum)
			//println(">>>> bd <= math.MaxInt8", bd)
			rvm := int64(0)
			rvn = reflect.ValueOf(&rvm)
		case bd == 0xc0:
			//println(">>>> 0xc0", bd)
			rvn = reflect.Zero(intfTyp)
		case bd == 0xc2 || bd == 0xc3:
			//println(">>>> bd == 0xc2 || bd == 0xc3", bd)
			rvm := false
			rvn = reflect.ValueOf(&rvm)
		case bd == 0xca || bd == 0xcb:
			//println(">>>> bd == 0xca || bd == 0xcb", bd)
			rvm := float64(0)
			rvn = reflect.ValueOf(&rvm)
		case bd >= 0xcc && bd <= 0xcf:
			//println(">>>> bd >= 0xcc && bd <= 0xcf", bd)
			rvm := uint64(0)
			rvn = reflect.ValueOf(&rvm)
		case bd >= 0xd0 && bd <= 0xd3:
			//println(">>>> bd >= 0xd0 && bd <= 0xd3", bd)
			rvm := int64(0)
			rvn = reflect.ValueOf(&rvm)
		case bd == 0xda || bd == 0xdb:
			//println(">>>> bd == 0xda || bd == 0xdb", bd)
			rvn = d.bytesAsStringFn(40)
		case bd == 0xdc || bd == 0xdd:
			//println(">>>> bd == 0xdc || bd == 0xdd", bd)
			rvm := make([]interface{}, 0, 20)
			rvn = reflect.ValueOf(&rvm)
		case bd == 0xde || bd == 0xdf:
			//println(">>>> bd == 0xde || bd == 0xdf", bd)
			rvn = d.makeMapFn(20)
		case (bd & 0xa0) == 0xa0:
			//case (bd | 0xa0) == bd:
			//println(">>>> (bd & 0xa0) == 0xa0", bd)
			rvn = d.bytesAsStringFn(8)
		case (bd & 0x90) == 0x90:
			//case (bd | 0x90) == bd:
			//println(">>>> (bd & 0x90) == 0x90", bd)
			rvm := make([]interface{}, 0, 4)
			rvn = reflect.ValueOf(&rvm)
		case (bd & 0x80) == 0x80:
			//case (bd | 0x80) == bd:
			//println(">>>> (bd & 0x80) == 0x80", bd)
			rvn = d.makeMapFn(4)
		default:
			panic(fmt.Errorf("%s: Nil-Deciphered DecodeValue: %s: %v", 
				msgTagDec, msgBadDesc, bd))
		}
		rv.Set(rvn) 
	}
	
	if bd == 0xc0 {
		rv2 := indir(rv, nil, -1)
		//fmt.Printf("$$$$$$$$ rv: %v, %v, %#v; rvt: %v, %#v\n", 
		//rv2.IsValid(), rv2, rv2, rvt2, rvt2)
		rv2.Set(reflect.Zero(rv2.Type()))
		return
	}
	
	switch rk := rv.Kind(); rk {
	case reflect.Ptr, reflect.Interface:
		d.decodeValue(bd, false, rv.Elem())
	case reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		switch bd {
		case 0xc2:
			rv.SetBool(false)
		case 0xc3:
			rv.SetBool(true)
			
		case 0xca:
			rv.SetFloat(float64(math.Float32frombits(uint32(d.readUint64(4)))))
		case 0xcb:
			rv.SetFloat(math.Float64frombits(d.readUint64(8)))
			
		case 0xcc:
			rv.SetUint(d.readUint64(1))
		case 0xcd:
			rv.SetUint(d.readUint64(2))
		case 0xce:
			rv.SetUint(d.readUint64(4))
		case 0xcf:
			rv.SetUint(d.readUint64(8))
			
		case 0xd0:
			rv.SetInt(d.readInt64(1))
		case 0xd1:
			rv.SetInt(d.readInt64(2))
		case 0xd2:
			rv.SetInt(d.readInt64(4))
		case 0xd3:
			rv.SetInt(d.readInt64(8))

		default:
			//may be a single-byte integer
			defHandled := false
			switch rk {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				sbd := int8(bd)
				if sbd > -32 && sbd <= math.MaxInt8 {
					rv.SetInt(int64(sbd))
					defHandled = true
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if bd <= math.MaxInt8 {
					rv.SetUint(uint64(bd))
					defHandled = true
				}
			}
			if !defHandled {
				panic(fmt.Errorf("%s: DefNotHandled DecodeValue: %s: %v", 
					msgTagDec, msgBadDesc, bd))
			}
		}
	case reflect.String:
		sbs := make([]byte, 0, 16)
		sbsrv := reflect.ValueOf(&sbs) //ensure you pass in a pointer
		d.decodeValue(bd, false, sbsrv)
		//rv.SetString(string(sbsrv.Interface().([]byte)))
		rv.SetString(string(sbs))
	case reflect.Slice, reflect.Array:
		rawbytes := false
		if rv.Type() == byteSliceTyp {
			rawbytes = true
		}
		l := d.readContainerLen(bd, false, rawbytes, true)
		if rawbytes {
			bs := rvi.([]byte)
			if len(bs) < l {
				bs = make([]byte, l)
				rv.Set(reflect.ValueOf(bs))
			}
			d.readb(l, bs)
			break
		}
		rvn := rv
		rvnElemTyp := rvn.Type().Elem()
		for j := 0; j < l; j++ {
			var rvj reflect.Value
			if rvn.Len() > j {
				rvj = rvn.Index(j)
				d.decodeValue(0, true, rvj)
			} else {
				rvj = reflect.New(rvnElemTyp)
				d.decodeValue(0, true, rvj)
				rvn = reflect.Append(rvn, rvj.Elem())
			}
			if rvnElemTyp == intfTyp {
				rvj = indir(rvj, nil, -1)
				if isMask(d.OptionsMask, MASK_B2S_SLICE_ELEMENT) && 
					rvj.Type() == byteSliceTyp {
					rvj = reflect.ValueOf(string(rvj.Interface().([]byte)))
				}
				rvn.Index(j).Set(rvj)
			}
		}
		rv.Set(rvn)
	case reflect.Map, reflect.Struct:
		if rk == reflect.Struct {
			//treat time.Time specially
			knownHandled := true
			switch rvi.(type) {
			case time.Time:
				ttstr := ""
				d.decodeValue(bd, false, reflect.ValueOf(&ttstr))
				tt, err := time.Parse(time.RFC3339, ttstr)
				if err != nil {
					panic(err)
				}
				rv.Set(reflect.ValueOf(tt))
			default:
				knownHandled = false
			}
			if knownHandled {
				break
			}
		}
		isMap := (rk == reflect.Map)
		l := d.readContainerLen(bd, false, false, false)
		var rvk, rvv reflect.Value
		for j := 0; j < l; j++ {
			if isMap {
				if rv.IsNil() {
					rvn := reflect.MakeMap(rv.Type())
					rv.Set(rvn)
				}
				ktype, vtype := rv.Type().Key(), rv.Type().Elem()
				rvk = reflect.New(ktype)
				d.decodeValue(0, true, rvk)
				rvk = rvk.Elem()
				//fmt.Printf("MMMMM map key: key type: %v, value type: %v, kind: %v" +
				//", value: %#v\n", ktype, vtype, rvk.Kind(), rvk.Interface())
				
				if ktype == intfTyp {
					rvk = indir(rvk, nil, -1)
					//map key (CANNOT) be a byte slice, so change to string
					if rvk.Type() == byteSliceTyp {
						rvk = reflect.ValueOf(string(rvk.Interface().([]byte)))
					}
				}
				rvv = rv.MapIndex(rvk)
				if rvv.IsValid() {
					d.decodeValue(0, true, rvv)
				} else {
					rvv = reflect.New(vtype)
					d.decodeValue(0, true, rvv)
					rvv = rvv.Elem()
				}
				if vtype == intfTyp {
					rvv = indir(rvv, nil, -1)
					if isMask(d.OptionsMask, MASK_B2S_MAP_VALUE) && 
						rvv.Type() == byteSliceTyp {
						rvv = reflect.ValueOf(string(rvv.Interface().([]byte)))
					}
				}
				rv.SetMapIndex(rvk, rvv)
			} else {
				//fmt.Printf("SSSSSS Decoding\n")
				rvkencname := ""
				rvk = reflect.ValueOf(&rvkencname)
				d.decodeValue(0, true, rvk)
				//rvkencname := rvk.String()
				var rvksi *structFieldInfo
				for _, si := range getStructFieldInfos(rv.Type()) {
					if si.encName == rvkencname {
						rvksi = si
						break
					}
				}
				if rvksi == nil {
					panic(fmt.Errorf("%s: DecodeValue: Invalid Enc Field: %s",
						msgTagDec, rvkencname))
				}
				//rvv = rv.FieldByName(rvkfieldname)
				rvv = rv.Field(rvksi.i)
				d.decodeValue(0, true, rvv)
			}
		}
	}
	return
}

// read a number of bytes into d.t, with error checking.
func (d *Decoder) read(numbytes int) {
	d.t = d.readb(numbytes, d.t)
}

// read a number of bytes into bs, and return an appropriate
// []byte with length adjusted.
func (d *Decoder) readb(numbytes int, bs []byte) []byte {
	numbytes, bs = checkByteSlice(numbytes, bs, true)
	n, err := d.r.Read(bs)
	switch {
	case err != nil:
		panic(err)
	case n != numbytes:
		panic(fmt.Errorf("%s: read: Incorrect num bytes read. Expecting: %v, Received: %v", 
			msgTagDec, numbytes, n))
	}
	return bs
}

func (d *Decoder) readInt64(numbytes int) (i int64) {
	d.read(numbytes)
	switch numbytes {
	case 1:
		i = int64(d.t[0])
	case 2:
		i = int64(int16(binary.BigEndian.Uint16(d.t)))
	case 4:
		i = int64(int32(binary.BigEndian.Uint32(d.t)))
	case 8:
		i = int64(binary.BigEndian.Uint64(d.t))
	default:
		panic(fmt.Errorf("Unacceptable num bytes read: %v", numbytes))
	}
	return
}

func (d *Decoder) readUint64(numbytes int) (i uint64) {
	d.read(numbytes)
	switch numbytes {
	case 1:
		i = uint64(d.t[0])
	case 2:
		i = uint64(binary.BigEndian.Uint16(d.t))
	case 4:
		i = uint64(binary.BigEndian.Uint32(d.t))
	case 8:
		i = binary.BigEndian.Uint64(d.t)
	default:
		panic(fmt.Errorf("%s: readUint64: Unacceptable num bytes read: %v", 
			msgTagDec, numbytes))
	}
	return
}


func (d *Decoder) readContainerLen(bd byte, readDesc bool, rawbytes bool, slicelike bool) (l int) {
	// bd is the byte descriptor. First byte is always descriptive.
	if readDesc {
		d.read(1)
		bd = d.t[0]
	}
	var b0, b1, b2  byte = 0x80, 0xde, 0xdf 
	if rawbytes {
		b0, b1, b2 = 0xa0, 0xda, 0xdb
	} else if slicelike { 
		b0, b1, b2 = 0x90, 0xdc, 0xdd
	}
	//fmt.Printf("bd: %d, bd: %x, b0: %x, b1: %x, b2: %x, b0&bd: %x\n", bd, bd, b0, b1, b2, b0&bd)
	switch {
	case bd == b1:
		l = int(d.readUint64(2))
	case bd == b2:
		l = int(d.readUint64(4))
	case (b0 & bd) == b0:
		l = int(b0 ^ bd)
	default:
		panic(fmt.Errorf("%s: readContainerLen: %s: %v", msgTagDec, msgBadDesc, bd))
	}
	return
	
}

// Unmarshal is a convenience function which decodes a stream of bytes into v.
// It delegates to Decoder.Decode.
func Unmarshal(data []byte, v interface{}) error {
	return NewDecoder(bytes.NewBuffer(data)).Decode(v)
}


