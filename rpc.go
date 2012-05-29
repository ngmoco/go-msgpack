
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
RPC

An RPC Client and Server Codec is implemented, so that msgpack can be used
with the standard net/rpc package. It supports both a basic net/rpc serialization,
and the custom format defined at http://wiki.msgpack.org/display/MSGPACK/RPC+specification

*/
package msgpack

import (
	"fmt"
	"net/rpc"
	"io"
)

type rpcCodec struct {
	rwc       io.ReadWriteCloser
	dec       *Decoder
	enc       *Encoder
	fmt       rpcFormat
}

// rpcFormat determines the type of RPC serialization we use.
// We use this instead of a boolean, so we can extend the set later without
// breaking the published API.
type rpcFormat byte

const (
	// BasicRPC format uses basic net/rpc serialization 
	BasicRPC = rpcFormat('B')
	// CustomRPC format uses custom format defined at http://wiki.msgpack.org/display/MSGPACK/RPC+specification .
	// The custom format support allows us talk to msgpack-rpc compatible clients and servers.
	CustomRPC = rpcFormat('C')
)

var unknownRPCFmtErr = "Msgpack: Unknown RPC Format: %v"

// NewRPCCodec returns a Codec which satisfies both rpc.ClientCodec and rpc.ServerCodec,
// allowing us use msgpack serialization for rpc communication.
// 
// Sample Usage:
//   conn, err = net.Dial("tcp", "localhost:5555")
//   codec, err := msgpack.NewRPCCodec(msgpack.BasicRPC, conn, nil)
//   // or either one below to be specific
//   // var codec rpc.ClientCodec = msgpack.NewRPCCodec(...)
//   // var codec rpc.ServerCodec = msgpack.NewRPCCodec(...)
//   client := rpc.NewClientWithCodec(codec)
//   ... (see rpc package for how to use an rpc client)
func NewRPCCodec(rpcfmt rpcFormat, conn io.ReadWriteCloser, opts DecoderContainerResolver) (r *rpcCodec, err error) {
	switch rpcfmt {
	case BasicRPC, CustomRPC:
	default:
		err = fmt.Errorf(unknownRPCFmtErr, rpcfmt)
		return
	}
	r = &rpcCodec{
		rwc: conn,
		dec: NewDecoder(conn, opts),
		enc: NewEncoder(conn),
		fmt: rpcfmt,
	}
	return
}

func (c *rpcCodec) Close() error {
	return c.rwc.Close()
}

func (c *rpcCodec) WriteRequest(r *rpc.Request, body interface{}) error {
	switch c.fmt {
	case BasicRPC:
		return c.write(r, body)
	case CustomRPC:
		return c.writeCustomBody(0, r.Seq, r.ServiceMethod, body)
	}
	return fmt.Errorf(unknownRPCFmtErr, c.fmt)
}

func (c *rpcCodec) WriteResponse(r *rpc.Response, body interface{}) error {
	switch c.fmt {
	case BasicRPC:
		return c.write(r, body)
	case CustomRPC:
		return c.writeCustomBody(1, r.Seq, r.Error, body)
	}
	return fmt.Errorf(unknownRPCFmtErr, c.fmt)
}

func (c *rpcCodec) ReadResponseBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *rpcCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *rpcCodec) ReadResponseHeader(r *rpc.Response) error {
	switch c.fmt {
	case BasicRPC:
		return c.dec.Decode(r)
	case CustomRPC:
		return c.parseCustomHeader(1, &r.Seq, &r.Error)
	}
	return fmt.Errorf(unknownRPCFmtErr, c.fmt)
}

func (c *rpcCodec) ReadRequestHeader(r *rpc.Request) error {
	switch c.fmt {
	case BasicRPC:
		return c.dec.Decode(r)
	case CustomRPC:
		return c.parseCustomHeader(0, &r.Seq, &r.ServiceMethod)
	}
	return fmt.Errorf(unknownRPCFmtErr, c.fmt)
}

func (c *rpcCodec) parseCustomHeader(expectTypeByte byte, msgid *uint64, methodOrError *string) (err error) {

	// We read the response header by hand 
	// so that the body can be decoded on its own from the stream at a later time.

	bs := make([]byte, 1)
	n, err := c.rwc.Read(bs)
	if err != nil {
		return 
	}
	if n != 1 {
		err = fmt.Errorf("Couldn't read array descriptor: No bytes read")
		return
	}
	const fia byte = 0x94 //four item array descriptor value
	if bs[0] != fia {
		err = fmt.Errorf("Unexpected value for array descriptor: Expecting %v. Received %v", fia, bs[0])
		return
	}
	var b byte
	if err = c.read(&b, msgid, methodOrError); err != nil {
		return
	}
	if b != expectTypeByte {
		err = fmt.Errorf("Unexpected byte descriptor in header. Expecting %v. Received %v", expectTypeByte, b)
		return
	}
	return
}

func (c *rpcCodec) writeCustomBody(typeByte byte, msgid uint64, methodOrError string, body interface{}) (
	err error) {
	//if response and error is defined, then ensure body is nil
	if typeByte == 1 && methodOrError != "" && body != nil {
		body = nil
	} 
	r2 := []interface{}{ typeByte, uint32(msgid), methodOrError, body }
	return c.enc.Encode(r2)
}

func (c *rpcCodec) write(objs ...interface{}) (err error) {
	for _, obj := range objs {
		if err = c.enc.Encode(obj); err != nil {
			return
		}
	}
	return
}

func (c *rpcCodec) read(objs ...interface{}) (err error) {
	for _, obj := range objs {
		if err = c.dec.Decode(obj); err != nil {
			return
		}
	}
	return
}

