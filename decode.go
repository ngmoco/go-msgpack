
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

// Some tagging information for error messages.
var (
	msgTagDec = "msgpack.decoder"
	msgBadDesc = "Unrecognized descriptor byte: "
)

// A Decoder reads and decodes an object from an input stream in the msgpack format.
type Decoder struct {
	r io.Reader
	opt DecoderOptions
	t []byte        //temp byte array re-used internally for efficiency
}

// DecoderOptions determine how to decode into a nil interface{}.
// They could be used to enforce JSON compatibility in that regard.
// See NewDecoder() for more information.
type DecoderOptions struct {
	// If decoding into a nil interface{} and we detect a map in the stream,
	// we create a map of the type specified. It defaults to creating a 
	// map[interface{}]interface{} if not specified. 
	MapType reflect.Type
	// If decoding into a nil interface{} and we detect a slice/array in the stream,
	// we create a slice of the type specified. It defaults to creating a 
	// []interface{} if not specified. 
	SliceType reflect.Type
	// convert to a string if raw bytes are detected while decoding 
	// into a interface{},
	BytesStringLiteral bool
	// convert to a string if raw bytes are detected while decoding 
	// into a []interface{},
	BytesStringSliceElement bool
	// convert to a string if raw bytes are detected while decoding 
	// into a value in a map[XXX]interface{},
	BytesStringMapValue bool
	// when encoding a time to Unix, what resolution do we use.
	TimeResolution TimeResolution
}

// NewDecoder returns a Decoder for decoding a stream of bytes into an object.
// If nil options are passed, we use a Decoder with full "string mode" set:
//    option: SliceType: nil
//    option: MapType: nil
//    option: BytesStringLiteral:      true
//    option: BytesStringMapValue:     true
//    option: BytesStringSliceElement: true
//    option: TimeResolution:          USEC
// 
func NewDecoder(r io.Reader, opts *DecoderOptions) (*Decoder) {
	//04-12: don't default to keep map_keys_string, due to 
	//Concerns with map[interface{}]interface{} in embedded recursive structs.
	if opts == nil {
		opts = &DecoderOptions{nil, nil, true, true, true, USEC}
	} 
	return &Decoder{r, *opts, make([]byte, 0, 16) }
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
//    - Maps are decoded as map[interface{}]interface{} 
//      unless you provide a default map type when creating your decoder.
//      option: MapType
//    - Lists are always decoded as []interface{}
//      unless you provide a default map type when creating your decoder.
//      option: SliceType
//    - raw bytes are decoded into []byte or string depending on setting of:
//      option: BytesStringLiteral
//      option: BytesStringMapValue
//      option: BytesStringSliceElement
//    - We still decode integers, floats, bool, Null, into these containers out of the stream.
// 
// time.Time is handled transparently. We will encode to and from a int64 Unix time 
// (ie number of nanoseconds since Jan 1, 1970 Epoch) based on the resolution in the 
// DecoderOptions.
// 
// Sample usages:
//   // Decoding into a non-nil typed value
//   var f float32
//   err = msgpack.NewDecoder(r).Decode(&f)
//
//   // Decoding into nil interface
//   var v interface{}
//   dec := msgpack.NewDecoder(r)
//   // To configure default options, use NewDecoderOptions(...)
//   dec := msgpack.NewDecoderOptions(r, nil, true, true, true)
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
		err = fmt.Errorf("%v: DecodeValue: Expecting valid pointer to decode into. Got: %v, %T, %v",
			msgTagDec, rv.Kind(), rv.Interface(), rv.Interface())
		return
	}

	//if a nil pointer is passed, set rv to the underlying value (not pointer).
	d.decodeValueT(rv.Elem(), true)
	return
}

func (d *Decoder) decodeValueT(rve reflect.Value, setToRealValue bool) (rvn reflect.Value) {
	wasNilIntf, rv := d.decodeValue(0, -1, true, rve)
	rvn = rve
	//if wasNilIntf, rv is either a pointer to actual value, a map or slice, or nil/invalid
	if wasNilIntf && rv.IsValid() {
		if rv.Kind() == reflect.Ptr {
			rv = rv.Elem()
		}
		if setToRealValue {
		   rve.Set(rv)
		}
		rvn = rv
	}
	return
}

func (d *Decoder) bytesOrStringFn(slsize int) (rvn reflect.Value) {
	if d.opt.BytesStringLiteral {
		rvm := ""
		rvn = reflect.ValueOf(&rvm)
	} else {
		rvm := make([]byte, slsize)
		rvn = reflect.ValueOf(&rvm)
	}
	return
}

func (d *Decoder) makeMapFn(msize int) (rvn reflect.Value) {
	if d.opt.MapType != nil {
		rvn = reflect.MakeMap(d.opt.MapType)
	} else {
		rvn = reflect.MakeMap(mapIntfIntfTyp)
	}
	return
}

func (d *Decoder) makeSliceFn(msize int) (rvn reflect.Value) {
	if d.opt.SliceType != nil {
		rvn = reflect.MakeSlice(d.opt.SliceType, msize, msize)
	} else {
		rvn = reflect.MakeSlice(intfSliceTyp, msize, msize)
	}
	return
}


func (d *Decoder) decodeValue(bd byte, containerLen int, readDesc bool, 
	rv0 reflect.Value) (wasNilIntf bool, rv reflect.Value) {
	// Separating this into multiple functions increased runtime 
	// from 380ms/op to 600ms/op. So keeping as single function.
	
	rv = rv0
	if readDesc {
		d.read(1)
		bd = d.t[0]
	}

	rk := rv.Kind()
	//wasNilIntf = rk == reflect.Interface && rv.Interface() == nil
	wasNilIntf = rk == reflect.Interface && rv.IsNil()

	//if nil interface, use some hieristics to set the nil interface to an 
	//appropriate value based on the first byte read (byte descriptor bd)
	if wasNilIntf {
		//if we set the reflect.Value to an primitive value, consider it handled and return.
		handled := true
		switch bd {
		case 0xc0:
		case 0xc2:
			rv.Set(reflect.ValueOf(false))
		case 0xc3:
			rv.Set(reflect.ValueOf(true))

		case 0xca:
			rv.Set(reflect.ValueOf(math.Float32frombits(d.readUint32())))
		case 0xcb:
			rv.Set(reflect.ValueOf(math.Float64frombits(d.readUint64())))
			
		case 0xcc:
			rv.Set(reflect.ValueOf(d.readUint8()))
		case 0xcd:
			rv.Set(reflect.ValueOf(d.readUint16()))
		case 0xce:
			rv.Set(reflect.ValueOf(d.readUint32()))
		case 0xcf:
			rv.Set(reflect.ValueOf(d.readUint64()))
			
		case 0xd0:
			rv.Set(reflect.ValueOf(int8(d.readUint8())))
		case 0xd1:
			rv.Set(reflect.ValueOf(int16(d.readUint16())))
		case 0xd2:
			rv.Set(reflect.ValueOf(int32(d.readUint32())))
		case 0xd3:
			rv.Set(reflect.ValueOf(int64(d.readUint64())))

		case 0xda, 0xdb:
			containerLen = d.readContainerLen(bd, false, true, true)
			rv.Set(d.bytesOrStringFn(containerLen))
			rv = rv.Elem()
			handled = false
		case 0xdc, 0xdd:
			containerLen = d.readContainerLen(bd, false, false, true)
			rv.Set(d.makeSliceFn(containerLen))
			handled = false
		case 0xde, 0xdf:
			containerLen = d.readContainerLen(bd, false, false, false)
			rv.Set(d.makeMapFn(containerLen))
			handled = false
		default:
			switch int8bd := int8(bd); {
			case int8bd > -32 || bd <= math.MaxInt8:
				//(Fixnum)
				rv.Set(reflect.ValueOf(int8bd))
			case (bd & 0xa0) == 0xa0:
				//case (bd | 0xa0) == bd:
				containerLen = d.readContainerLen(bd, false, true, true)
				rv.Set(d.bytesOrStringFn(containerLen))
				rv = rv.Elem()
				handled = false
			case (bd & 0x90) == 0x90:
				containerLen = d.readContainerLen(bd, false, false, true)
				rv.Set(d.makeSliceFn(containerLen))
				handled = false
			case (bd & 0x80) == 0x80:
				containerLen = d.readContainerLen(bd, false, false, false)
				rv.Set(d.makeMapFn(8))
				handled = false
			default:
				panic(fmt.Errorf("%s: Nil-Deciphered DecodeValue: %s: %x", 
					msgTagDec, msgBadDesc, bd))
			}
		}
		if handled {
			return
		}

		rk = rv.Kind()
	}
	
	if bd == 0xc0 {
		rv.Set(reflect.Zero(rv.Type()))	
		return
	}
	
	switch rk {
	case reflect.Ptr, reflect.Interface:
		d.decodeValue(bd, containerLen, false, rv.Elem())
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
			rv.SetFloat(float64(math.Float32frombits(d.readUint32())))
		case 0xcb:
			rv.SetFloat(math.Float64frombits(d.readUint64()))
			
		case 0xcc:
			rv.SetUint(uint64(d.readUint8()))
		case 0xcd:
			rv.SetUint(uint64(d.readUint16()))
		case 0xce:
			rv.SetUint(uint64(d.readUint32()))
		case 0xcf:
			rv.SetUint(d.readUint64())
			
		case 0xd0:
			rv.SetInt(int64(int8(d.readUint8())))
		case 0xd1:
			rv.SetInt(int64(int16(d.readUint16())))
		case 0xd2:
			rv.SetInt(int64(int32(d.readUint32())))
		case 0xd3:
			rv.SetInt(int64(d.readUint64()))

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
				panic(fmt.Errorf("%s: DefNotHandled DecodeValue: %s: %x", 
					msgTagDec, msgBadDesc, bd))
			}
		}
	case reflect.Slice, reflect.Array, reflect.String:
		rvtype := rv.Type()
		isString := rk == reflect.String
		isByteSlice := rvtype == byteSliceTyp
		rawbytes := isString || isByteSlice
		
		if containerLen < 0 {
			containerLen = d.readContainerLen(bd, false, rawbytes, true)
		}
		if containerLen == 0 {
			break
		}
		
		if rawbytes {
			var bs []byte
			if isByteSlice {
				//bs = rv.Interface().([]byte) //PERF: increased runtime from 320ms/op to 410ms/op
				//bs = rv.Bytes()              //PERF: increases runtime from 320ms/op to 390ms/op
				rvi := rv.Interface()
				bs = rvi.([]byte)
			}
			if len(bs) < containerLen {
				bs = make([]byte, containerLen)
				if isByteSlice {
					rv.Set(reflect.ValueOf(bs))
				}
			} else if len(bs) > containerLen {
				bs = bs[:containerLen]
			}
			d.readb(containerLen, bs)
			if isString {
				rv.SetString(string(bs))
			} 
			break
		}
		rvn := rv
		rvnTyp := rvn.Type()
		rvnElemTyp := rvnTyp.Elem()

		rvnlen := rvn.Len()
		switch {
		case rvn.IsNil():
			rvn = reflect.MakeSlice(rvnTyp, containerLen, containerLen)
		case containerLen > rvnlen:
			switch {
			case containerLen > rvn.Cap():
				rvn2 := reflect.MakeSlice(rvnTyp, containerLen, containerLen)
				if rvnlen > 0 {
					reflect.Copy(rvn2, rvn)
				}
				rvn = rvn2
			default:
				rvn.SetLen(containerLen)
			}
		}
		
		for j := 0; j < containerLen; j++ {
			rvj := rvn.Index(j)
			d.decodeValueT(rvj, true)
			if rvnElemTyp == intfTyp {
				rvj2 := rvj.Elem()
				if rvj2.IsValid() && 
					d.opt.BytesStringSliceElement && 
					rvj2.Type() == byteSliceTyp {
					rvj.Set(reflect.ValueOf(string(rvj2.Bytes())))
				}
			}
		}
		if rv != rvn {
			rv.Set(rvn)
		}
	case reflect.Struct:
		//if _, ok := rvi.(time.Time); ok {
		rvtype := rv.Type()
		if rvtype == timeTyp {
			tt := int64(0)
			d.decodeValue(bd, -1, false, reflect.ValueOf(&tt).Elem())
			tt = d.timeNano(tt)
			rv.Set(reflect.ValueOf(time.Unix(0, tt).UTC()))
			break
		}
		
		if containerLen < 0 {
			containerLen = d.readContainerLen(bd, false, false, false)
		}
		if containerLen == 0 {
			break
		}
		for j := 0; j < containerLen; j++ {
			rvkencname := ""
			rvk := reflect.ValueOf(&rvkencname).Elem()
			d.decodeValue(0, -1, true, rvk)
			rvksi := getStructFieldInfos(rvtype).getForEncName(rvkencname)
			if rvksi == nil {
				panic(fmt.Errorf("%s: DecodeValue: Invalid Enc Field: %s",
					msgTagDec, rvkencname))
			}
			rvv := rv.Field(rvksi.i)
			d.decodeValueT(rvv, true)
		}
	case reflect.Map:
		if containerLen < 0 {
			containerLen = d.readContainerLen(bd, false, false, false)
		}
		if containerLen == 0 {
			break
		}
		rvtype := rv.Type()
		ktype, vtype := rvtype.Key(), rvtype.Elem()			
		if rv.IsNil() {
			rvn := reflect.MakeMap(rvtype)
			rv.Set(rvn)
		}
		for j := 0; j < containerLen; j++ {
			rvk := reflect.New(ktype).Elem()
			rvk = d.decodeValueT(rvk, false)
			
			if ktype == intfTyp && rvk.Type() == byteSliceTyp {
				rvk = reflect.ValueOf(string(rvk.Bytes()))
			}
			rvv := rv.MapIndex(rvk)
			if !rvv.IsValid() {
				rvv = reflect.New(vtype).Elem()
			}
			rvv = d.decodeValueT(rvv, false)
			if d.opt.BytesStringMapValue &&
				vtype == intfTyp && 
				rvv.Type() == byteSliceTyp {
				rvv = reflect.ValueOf(string(rvv.Bytes()))
			}
			rv.SetMapIndex(rvk, rvv)
		}
	}
	return
}

func (d *Decoder) timeNano(tt int64) int64 {
	//TODO: Perf: moving this to its own function brought the time down dramatically.
	//from 600us to 396us.
	if tt < 0 {
		panic(fmt.Errorf("%s: Invalid Time decoded: %v", msgTagDec, tt))
	}
	//tt = tt * 1e3; return tt
	switch d.opt.TimeResolution {
	case SEC:  
		tt = tt * 1e9
	case MSEC: 
		tt = tt * 1e6
	case USEC: 
		tt = tt * 1e3
	case NSEC: 
	default: 
		panic(fmt.Errorf("%s: Invalid Time Resolution: %d", msgTagEnc, d.opt.TimeResolution))
	}
	return tt
}

// read a number of bytes into d.t, with error checking.
func (d *Decoder) read(numbytes int) {
	d.t = d.t[:numbytes]
	d.t = d.readb(numbytes, d.t)
}

// read a number of bytes into bs, and return an appropriate
// []byte with length adjusted.
func (d *Decoder) readb(numbytes int, bs []byte) []byte {
	n, err := d.r.Read(bs)
	if err != nil {
		panic(err)
	} 
	if n != numbytes {
		panic(fmt.Errorf("%s: read: Incorrect num bytes read. Expecting: %v, Received: %v", 
			msgTagDec, numbytes, n))
	}
	return bs
}

func (d *Decoder) readUint8() uint8 {
	d.read(1)
	return d.t[0]
}

func (d *Decoder) readUint16() uint16 {
	d.read(2)
	return binary.BigEndian.Uint16(d.t)
}

func (d *Decoder) readUint32() uint32 {
	d.read(4)
	return binary.BigEndian.Uint32(d.t)
}

func (d *Decoder) readUint64() uint64 {
	d.read(8)
	return binary.BigEndian.Uint64(d.t)
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

	switch {
	case bd == b1:
		l = int(d.readUint16())
	case bd == b2:
		l = int(d.readUint32())
	case (b0 & bd) == b0:
		l = int(b0 ^ bd)
	default:
		panic(fmt.Errorf("%s: readContainerLen: %s: %x", msgTagDec, msgBadDesc, bd))
	}
	return	
}

// Unmarshal is a convenience function which decodes a stream of bytes into v.
// It delegates to Decoder.Decode.
func Unmarshal(data []byte, v interface{}, opts *DecoderOptions) error {
	return NewDecoder(bytes.NewBuffer(data), opts).Decode(v)
}

// BUG(ugorji): During a read, we may not read enough bytes in 1 call. This is expected like if 
// using bufio.Reader. We may have to make more calls to get enough bytes during a read.

