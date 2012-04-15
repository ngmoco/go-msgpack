
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
	"time"
	"errors"
)

var (
	raisePanicAfterRecover = false

	structInfoFieldName = "_struct"
	
	cachedStructFieldInfos = make(map[reflect.Type]*structFieldInfos, 4)
	cachedStructFieldInfosMutex sync.Mutex

	byteSliceTyp = reflect.TypeOf([]byte(nil))
	intfSliceTyp = reflect.TypeOf([]interface{}(nil))
	intfTyp = intfSliceTyp.Elem()
	timeTyp = reflect.TypeOf(time.Time{})
	mapStringIntfTyp = reflect.TypeOf(map[string]interface{}(nil))
	mapIntfIntfTyp = reflect.TypeOf(map[interface{}]interface{}(nil))
)

type structFieldInfo struct {
	i         int // field index in struct
	tag       string
	omitEmpty bool
	skip      bool
	encName   string // encode name
	name      string // field name
}

type structFieldInfos struct {
	sis []*structFieldInfo
	//encmap map[string]*structFieldInfo
}

func (sis *structFieldInfos) getForEncName(name string) *structFieldInfo {
	for _, si := range sis.sis {
		if si.encName == name {
			return si
		}
	}
	return nil
	//return sis.encmap[name]
}

func getStructFieldInfos(rt reflect.Type) (sis *structFieldInfos) {
	var ok bool
	if sis, ok = cachedStructFieldInfos[rt]; ok {
		return 
	}
	
	cachedStructFieldInfosMutex.Lock()
	defer cachedStructFieldInfosMutex.Unlock()
	
	sis = new(structFieldInfos)
		
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
		sis.sis = append(sis.sis, si)
		//if sis.encmap == nil {
		//	sis.encmap = make(map[string]*structFieldInfo, rt.NumField())
		//}
		//sis.encmap[si.encName] = si
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

func approxDataSize(rv reflect.Value, inclContainerSize bool, inclNonDataSize bool) (sum int) {
	if !rv.IsValid() {
		return
	}
	switch rk := rv.Kind(); rk {
	case reflect.Invalid:
	case reflect.Chan, reflect.Func, reflect.UnsafePointer:
		if inclNonDataSize { sum += int(rv.Type().Size()) }
	case reflect.Ptr, reflect.Interface:
		if inclNonDataSize { sum += int(rv.Type().Size()) }
		sum += approxDataSize(rv.Elem(), inclContainerSize, inclNonDataSize)
    case reflect.Array, reflect.Slice:
		if inclContainerSize { sum += int(rv.Type().Size()) }
		for j := 0; j < rv.Len(); j++ {
			sum += approxDataSize(rv.Index(j), inclContainerSize, inclNonDataSize)
		}
    case reflect.String:
		if inclContainerSize { sum += int(rv.Type().Size()) }
		sum += rv.Len()
    case reflect.Map:
		if inclContainerSize { sum += int(rv.Type().Size()) }
		for _, mk := range rv.MapKeys() {
			sum += approxDataSize(mk, inclContainerSize, inclNonDataSize)
			sum += approxDataSize(rv.MapIndex(mk), inclContainerSize, inclNonDataSize)
		}
    case reflect.Struct:
		if inclContainerSize { sum += int(rv.Type().Size()) }
		for j := 0; j < rv.NumField(); j++ {
			sum += approxDataSize(rv.Field(j), inclContainerSize, inclNonDataSize)
		}		
	default:
		sum += int(rv.Type().Size())
	}
	return
}

