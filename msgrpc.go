/*
Copyright (c) 2012, Damian Gryski <damian@gryski.com>
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
MsgpackRPC

An MsgpackRPC Client and Server Codec is implemented, so that Go can
interoperate with the standard msgpack rpc protocol.  This protocol is
documented in http://wiki.msgpack.org/display/MSGPACK/RPC+specification

See NewRPCCodec() documentation and see net/rpc package for 
further information on how to use the rpc package. 

*/
package msgpack

import (
	"errors"
	"fmt"
	"io"
	//"log"
	"net/rpc"
)

type msgrpcCodec struct {
	rwc io.ReadWriteCloser
	dec *Decoder
	enc *Encoder
}

const fourItemArray = 0x94

// NewMsgRPCCodec returns a Codec which satisfies both rpc.ClientCodec and rpc.ServerCodec,
// allowing us to talk to msgpack-rpc compatible clients and servers.
// 
// Sample Usage:
//   conn, err = net.Dial("tcp", "localhost:5555")
//   codec := msgpack.NewMsgRPCCodec(conn)
//   // or either one below to be specific
//   // var codec rpc.ClientCodec = msgpack.NewMsgRPCCodec(conn)
//   // var codec rpc.ServerCodec = msgpack.NewMsgRPCCodec(conn)
//   client := rpc.NewClientWithCodec(codec)
//   ... (see rpc package for how to use an rpc client)
// 
func NewMsgRPCCodec(conn io.ReadWriteCloser) *msgrpcCodec {
	//don't use a bufio Reader, because you don't want to read more than is necessary
	//this is because multiple requests can use one connection.
	return &msgrpcCodec{
		rwc: conn,
		dec: NewDecoder(conn, nil),
		enc: NewEncoder(conn, nil),
	}
}

func (c *msgrpcCodec) write(body interface{}) (err error) {
	if err = c.enc.Encode(body); err != nil {
		return
	}
	return
}

func (c *msgrpcCodec) WriteRequest(r *rpc.Request, body interface{}) error {

	request := make([]interface{}, 4)
	request[0] = byte(0)
	request[1] = uint32(r.Seq)
	request[2] = string(r.ServiceMethod)
	request[3] = []interface{}{body}

	return c.write(request)
}

func (c *msgrpcCodec) ReadResponseHeader(r *rpc.Response) error {

	// We read the response header by hand so that later we can decode the body directly into the destination.
	// Otherwise, we would have to decode the entire response and store the body for when it was actually requested.

	var b [1]byte

	if n, err := c.rwc.Read(b[:]); err != nil || n != 1 {
		//log.Println("error during response header read: ", err)
		return err
	}

	if b[0] != fourItemArray {
		return errors.New(fmt.Sprintf("unknown type in response header: %x", b[0]))
	}

	// We now know there are four things on the wire
	var one int64
	if err := c.dec.Decode(&one); err != nil {
		//log.Println("read response header failed")
		return err
	}

	if one != 1 {
		//log.Println("bad value in response header type: ", one)
		return errors.New("bad value in response header type")
	}

	if err := c.dec.Decode(&r.Seq); err != nil {
		//log.Println("error reading response sequence number")
		return err
	}

	if err := c.dec.Decode(&r.Error); err != nil {
		//log.Println("error reading response error field")
		return err
	}

	// leave the response body on the wire for later

	return nil
}

func (c *msgrpcCodec) ReadResponseBody(body interface{}) (err error) {

	if err = c.dec.Decode(body); err != nil {
		//log.Println("error decoding body: ", err)
		return
	}

	return
}

func (c *msgrpcCodec) ReadRequestHeader(r *rpc.Request) error {

	var b [1]byte

	if n, err := c.rwc.Read(b[:]); err != nil || n != 1 {
		return err
	}

	if b[0] != fourItemArray {
		return errors.New(fmt.Sprintf("unknown type in request header: %x", b[0]))
	}

	var zero int64

	if err := c.dec.Decode(&zero); err != nil {
		//log.Println("read request header failed:", err)
		return err
	}

	if zero != 0 {
		//log.Println("bad value in request header type: ", one)
		return errors.New("bad value in request header type")
	}

	if err := c.dec.Decode(&r.Seq); err != nil {
		//log.Println("error reading sequence number:", err)
		return err
	}

	if err := c.dec.Decode(&r.ServiceMethod); err != nil {
		//log.Println("error reading service method:", err)
		return err
	}

	// leave the request body on the wire for later
	return nil

}

func (c *msgrpcCodec) ReadRequestBody(body interface{}) error {

	var params = []interface{}{body}

	if err := c.dec.Decode(&params); err != nil {
		//log.Println("error decoding request body: ", err)
		return err
	}

	return nil
}

func (c *msgrpcCodec) WriteResponse(r *rpc.Response, body interface{}) error {

	response := make([]interface{}, 4)

	response[0] = byte(1)
	response[1] = uint32(r.Seq)

	// precisely one of error or body can be non-nil
	if r.Error == "" {
		response[2] = nil
		response[3] = body
	} else {
		response[3] = r.Error
		response[3] = nil
	}

	return c.write(response)
}

func (c *msgrpcCodec) Close() error {
	return c.rwc.Close()
}
