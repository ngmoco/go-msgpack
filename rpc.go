
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
with the standard net/rpc package. 

See NewRPCCoder() documentation and see net/rpc package for 
further information on how to use the rpc package. 

*/
package msgpack

import (
	"net/rpc"
	"io"
)

type rpcCodec struct {
	rwc    io.ReadWriteCloser
	dec    *Decoder
	enc    *Encoder
}

// NewRPCCodec returns a Codec which satisfies both rpc.ClientCodec and rpc.ServerCodec,
// allowing us use msgpack serialization for rpc communication.
// 
// Sample Usage:
//   conn, err = net.Dial("tcp", "localhost:5555")
//   codec := msgpack.NewRPCCodec(conn)
//   // or either one below to be specific
//   // var codec rpc.ClientCodec = msgpack.NewRPCCodec(conn)
//   // var codec rpc.ServerCodec = msgpack.NewRPCCodec(conn)
//   client := rpc.NewClientWithCodec(codec)
//   ... (see rpc package for how to use an rpc client)
// 
func NewRPCCodec(conn io.ReadWriteCloser) *rpcCodec {
	//don't use a bufio Reader, because you don't want to read more than is necessary
	//this is because multiple requests can use one connection.
	return &rpcCodec{
		rwc: conn,
		dec: NewDecoder(conn, nil),
		enc: NewEncoder(conn, nil),
	}
}

func (c *rpcCodec) write(header interface{}, body interface{}) (err error) {
	if err = c.enc.Encode(header); err != nil {
		return
	}
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return
}

func (c *rpcCodec) WriteRequest(r *rpc.Request, body interface{}) error {
	return c.write(r, body)
}

func (c *rpcCodec) ReadResponseHeader(r *rpc.Response) error {
	return c.dec.Decode(r)
}

func (c *rpcCodec) ReadResponseBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *rpcCodec) ReadRequestHeader(r *rpc.Request) error {
	return c.dec.Decode(r)
}

func (c *rpcCodec) ReadRequestBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *rpcCodec) WriteResponse(r *rpc.Response, body interface{}) error {
	return c.write(r, body)
}

func (c *rpcCodec) Close() error {
	return c.rwc.Close()
}

