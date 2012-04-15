
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

/*
MsgPack library for Go.

http://wiki.msgpack.org/display/MSGPACK/Format+specification

It provides features similar to encoding packages in the standard library (ie json, xml, gob, etc).

Supports:
  - Standard Marshal/Unmarshal interface.
  - Standard field renaming via tags
  - Encoding from any value (struct, slice, map, primitives, pointers, interface{}, etc)
  - Decoding into pointer to any non-nil value (struct, slice, map, int, float32, bool, string, etc)
  - Decoding into a nil interface{} (big)
  - Handles time.Time transparently (as int64 UnixNano to specified resolution)

Usage

  dec = msgpack.NewDecoder(r, nil)
  err = dec.Decode(&v) 
  
  enc = msgpack.NewEncoder(w, nil)
  err = enc.Encode(v) 
  
  //methods below are convenience methods over functions above.
  data, err = msgpack.Marshal(v, nil) 
  err = msgpack.Unmarshal(data, &v, nil)

JSON Compatibility

The Decoder takes options that can allow you specify that 
your resulting map should be JSON compatible. For example,
you can specify that all []byte be converted to strings and
all maps be of type map[string]interface{}.

See NewDecoder(...) documentation.

*/
package msgpack


// BUG(ugorji): This package ignores anonymous (embedded) struct fields during encoding and decoding. 
// This is in line with the behaviour of Json and Xml packages as of Go 1. 

