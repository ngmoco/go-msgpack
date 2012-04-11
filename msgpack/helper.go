
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

// All non-std package dependencies live in this file,
// so porting to different environment is easy (just update functions).

import (
	"unicode"
	"unicode/utf8"
	"reflect"
	"sync"
	"strings"
	"fmt"
	"errors"
	"math"
)

var (
	raisePanicAfterRecover = true
	structInfoFieldName = "_struct"
	
	cachedStructFieldInfos = make(map[reflect.Type][]*structFieldInfo, 4)
	cachedStructFieldInfosMutex sync.Mutex
	
	byteSliceTyp = reflect.TypeOf([]byte(nil))
	intfSliceTyp = reflect.TypeOf([]interface{}(nil))
	intfTyp = intfSliceTyp.Elem()
)

type structFieldInfo struct {
	i         int // field index in struct
	tag       string
	omitEmpty bool
	skip      bool
	encName   string // encode name
	name      string // field name
}

func getStructFieldInfos(rt reflect.Type) (sis []*structFieldInfo) {
	var ok bool
	if sis, ok = cachedStructFieldInfos[rt]; ok {
		return 
	}

	cachedStructFieldInfosMutex.Lock()
	defer cachedStructFieldInfosMutex.Unlock()

	var si, siInfo *structFieldInfo
	if f, ok := rt.FieldByName(structInfoFieldName); ok {
		siInfo = parseStructFieldInfo(f)
		//fmt.Printf("siInfo.tag: %v, siInfo.omitEmpty: %v\n", siInfo.tag, siInfo.omitEmpty)
	}
	for j := 0; j < rt.NumField(); j++ {
		f := rt.Field(j)
		si = parseStructFieldInfo(f)
		if si.skip {
			continue
		}
		if siInfo != nil {
			if siInfo.omitEmpty {
				si.omitEmpty = true
			}
		}
		sis = append(sis, si)
	}
	cachedStructFieldInfos[rt] = sis
	return
}

func parseStructFieldInfo(f reflect.StructField) (si *structFieldInfo) {
	si = &structFieldInfo{
		i: f.Index[0],
		name: f.Name,
		encName: f.Name,
	}
	
	//TODO: Skip anonymous fields for now, until JSON, etc decide what to do with them
	if f.Anonymous {
		si.skip = true
	} else {
		rune1, _ := utf8.DecodeRuneInString(si.name)
		if rune1 == utf8.RuneError || !unicode.IsUpper(rune1) {
			si.skip = true
		} 
	}
	
	si.tag = f.Tag.Get("msgpack")
	switch si.tag {
	case "":
	case "-":
		si.skip = true
	default:
		for i, s := range strings.Split(si.tag, ",") {
			if i == 0 {
				if s != "" {
					si.encName = s
				}
				continue
			}
			switch s {
			case "omitempty":
				si.omitEmpty = true
			}
		}
	}
	return
}

func reflectValue(v interface{}) (rv reflect.Value) {
	if rv2, ok := v.(reflect.Value); ok {
		rv = rv2
	} else {
		rv = reflect.ValueOf(v)
	}
	return 
}

// checkByteSlice will try to ensure returned byte length = numbytes.
// It may expand the []byte up to the capacity.
// It panics if any checks don't pass.
// Encoder.write and Decoder.read both use it.
func checkByteSlice(numbytes int, bs []byte, expand bool) (int, []byte) {
	lenbs := len(bs)
	if numbytes < 0 {
		numbytes = lenbs
	}
	switch {
	case numbytes > lenbs:
		capbs := cap(bs)
		if capbs < numbytes {
			panic(fmt.Errorf("%s: Requesting more bytes than in slice capacity: %v, " + 
				"Slice Len: %v, Cap: %v", 
				"msgpack.checkByteSlice", numbytes, lenbs, capbs))
		} else if expand {
			bs = bs[0:numbytes]
		} else {
			panic(fmt.Errorf("%s: Requesting more bytes than in slice length: %v, " + 
				"Slice Len: %v, Cap: %v", 
				"msgpack.checkByteSlice", numbytes, lenbs, capbs))
		}
	case numbytes < lenbs:
		bs = bs[0:numbytes]
	}
	return numbytes, bs
}

func isMask(v int, mask int) bool {
	return (v & mask) == mask
}

func panicToErr(err *error) {
	if x := recover(); x != nil { 
		panicToErrT(x, err)
		if raisePanicAfterRecover {
			panic(x)
		}
	}
}

func panicToErrT(panicVal interface{}, err *error) {
	switch xerr := panicVal.(type) {
	case error:
		*err = xerr
	case string:
		*err = errors.New(xerr)
	default:
		*err = fmt.Errorf("%v", panicVal)
	}
	//panic(panicVal)
	return
}

func indir(rv reflect.Value, finalTyp reflect.Type, maxDepth int) reflect.Value {
	if !rv.IsValid() {
		return rv
	}
	//treat intfType as nil (and just flatten all the way)
	if finalTyp == intfTyp {
		finalTyp = nil
	}
	if maxDepth <= 0 {
		maxDepth = math.MaxInt16
	}
	for i := 0; i < maxDepth; i++ {
		if finalTyp != nil && rv.Type() == finalTyp {
			break
		}
		rk := rv.Kind()
		if !(rk == reflect.Ptr || rk == reflect.Interface) {
			break
		}
		rv2 := rv.Elem()
		if !rv2.IsValid() {
			break
		}
		rv = rv2
	}
	return rv
}

func isEmptyValue(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	case reflect.Bool:
		return !v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return v.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return v.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return v.IsNil()
	}
	return false
}


