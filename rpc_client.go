/*
Copyright (c) 2012, ngmoco:)
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

import (
	"net"
	"sync"
	"sync/atomic"
	"log"
	"errors"
	"reflect"
)

/* The Client class reimplements the RPC protocol but adds pipelining support
 * which is not supported by the net/rpc general RPC impelementation
 */
type Client struct {
	conn net.Conn
	dec *Decoder
	enc *Encoder
	msgid uint32
	seqMutex sync.Mutex
	sendChan chan []interface{}
	replyCheckChan chan replyCheck
	replyChanInternal chan ClientResponse
	dieChan chan error
	closed int32
	cce *ClientClosedError
}

type ClientResponse struct {
	Msgid uint32
	Error error
	Result interface{}
}

type replyCheck struct {
	msgid uint32
	replyChan chan ClientResponse
}

func NewClient(conn net.Conn, dopts *DecoderOptions, eopts *EncoderOptions) (*Client) {
	return NewClientWithOptions(conn, nil, nil)
}

func NewClientWithOptions(conn net.Conn, dopts *DecoderOptions, eopts *EncoderOptions) (*Client) {
	c := &Client{
		conn: conn,
		dec: NewDecoder(conn, dopts),
		enc: NewEncoder(conn, eopts),
		msgid: 1,
		sendChan: make(chan []interface{}, 100),
		// unbuffered to make sure we have it on the reply side before we send the message
		replyCheckChan: make(chan replyCheck),
		replyChanInternal: make(chan ClientResponse, 100),
		dieChan: make(chan error, 3), // I want to make sure that both send and recieve don't block and exit their loops
	}
	go c.receiver()
	go c.sender()
	return c
}

// Send a request and wait for a response
// @return the result data or error if there is an error
//    if the error is of type ClientClosedError, then the client has ahd a fatal error and can no longer be used.
func (c *Client) Send(method string, params []interface{}) (interface{}, error) {
	replyChan := make(chan ClientResponse)
	// ignore msgid since i'm creating a new chan every time
	_, err := c.SendAsync(method, params, replyChan)
	if err == nil {
		reply := <-replyChan
		return reply.Result, reply.Error
	} else {
		return nil, err
	}
	// unreachable
	return nil, nil
}

// Send a request but don't wait for the response. This call may block if a large number of requests
// are backed up on the internal buffered channel (currently size 100).  
// @param replyChan this channel may be new for every request, or a shared channel may be used. Be sure to 
//    use a buffered channel or immediately wait for the response because the message will be thrown away if the
//    reply cannot be sent immediately to the channel 
// @return message id of the request.  it is useful for demultiplexing requests if using a single reply channel.  err will
//    always be nil unless the Client has closed due to an error.  If you get a ClientClosedError
func (c *Client) SendAsync(method string, params []interface{}, replyChan chan ClientResponse)  (msgid uint32, err *ClientClosedError) {
	if c.closed == 1 {
		return 0, c.cce
	}
	
	// build the request
	request := make([]interface{}, 4)
	request[0] = byte(0)
	msgid = c.nextSequence()
	request[1] = msgid
	request[2] = method
	request[3] = params

	// first send the reply chan and id to the sender in a blocking manner
	// so that we are sure to have the reply channel mapped before the request
	// goes on the wire
	c.replyCheckChan <- replyCheck{msgid, replyChan}
	// send the request directly to the socket writer goroutine
	c.sendChan <- request
	return msgid, nil
}

// Grab the next sequence atomically to allow multiple
// goroutines to call a single client
func (c *Client) nextSequence() uint32 {
	// grab a sequence atomically
	c.seqMutex.Lock()
	tmp := c.msgid
	c.msgid++
	c.seqMutex.Unlock()
	return tmp
}

// sends on the socket and doesn't care about responses
func (c *Client) sender() {
	for req := range c.sendChan {
		log.Println("Got request to send")
		if c.closed == 1 {
			break
		}
		err := c.enc.Encode(req)
		if err != nil {
			c.dieChan <- err
			break
		}
	}
	log.Println("RPC client socket writer shutting down")
}

func (c *Client) receiver() {
	// holds a map of message ID's to their response channels
	replyMap := make(map[uint32]chan ClientResponse)
	first := true
	for c.closed != 1 {
		select {
		case replyCheck := <- c.replyCheckChan:
			// TODO: check map for lost messages (timed-out) and return errors
			replyMap[replyCheck.msgid] = replyCheck.replyChan

			// when I get the first reqest, start the socket receiver go routine
			if first {
				first = false
				go c.connRecv()
			}
		case reply := <- c.replyChanInternal:
			replyChan := replyMap[reply.Msgid]
			delete(replyMap, reply.Msgid)
			if replyChan != nil {
				// don't ever block on the user channel send
				select {
				case replyChan <- reply:
				default:
					log.Printf("Discarded message %v on busy channel", reply.Msgid)
				}
			}
		case err := <- c.dieChan:
			log.Println("Got a fatal error:", err)
			// shutdown everything and clear the rest of the responses with an error
			// this stops the senders
			c.cce = &ClientClosedError{err}
			c.Close()
			// this should break the IO loops if not already broken
			c.conn.Close()
			// sleep?
		}
	}
	c.cleanup(replyMap)
	log.Println("RPC Client closed to requests")
}

func (c *Client) Close() {
	atomic.StoreInt32(&c.closed, 1)
}

func (c *Client) cleanup(replyMap map[uint32]chan ClientResponse) {
	// Cleanup
	// The assumtion by this point is that the connRecv or sender goroutines have exited due to error
	// or closure
	// drain replyCheckChan drain these in the normal way so it unblocks any clients
	more := true
	for more {
		select {
		case replyCheck := <- c.replyCheckChan:
			replyMap[replyCheck.msgid] = replyCheck.replyChan
		default:
			// break if we run out
			more = false
		}
	}
	close(c.replyCheckChan)
	
	// drain replyChanInternal
	// These probably have valid responses so send them back normally
	more = true
	for more {
		select {
		case reply := <- c.replyChanInternal:
			replyChan := replyMap[reply.Msgid]
			delete(replyMap, reply.Msgid)
			if replyChan != nil {
				// don't ever block on the user channel send
				select {
				case replyChan <- reply:
				default:
					log.Printf("Discarded message %v on busy channel", reply.Msgid)
				}
			}
		default:
			// break if we run out
			more = false
		}		
	}
	close(c.replyChanInternal)
	
	// Drain sendChan to unblock clients if the send loop has died
	// just throw away the requests
	more = true
	for more {
		select {
		case <- c.sendChan:
		default:
			// break if we run out
			more = false
		}		
	}
	close(c.sendChan)
	
	for msgid, replyChan := range replyMap {
		delete(replyMap, msgid)
		select {
		case replyChan <- ClientResponse{msgid, c.cce, nil}:
		default:
			log.Printf("Discarded message %v on busy channel", msgid)
		}
	}
	
}

// this is only to listen to the recieve side of the socket
func (c *Client) connRecv() {
	for c.closed != 1 {
		var v interface{}
		err := c.dec.Decode(&v)
		if err != nil {
			c.dieChan <- err
			break
		}
		msgpack_reply, ok := v.([]interface{})
		// per spec, valid replies must be an array of length 4 containing
		// 0 : 1 (for a response message)
		// 1 : Message ID
		// 2 : Error
		// 3 : Data
		if ok && len(msgpack_reply) == 4 {
			// get the message ID and convert it to a uint32, it should be some type of int or uint
			val := reflect.ValueOf(msgpack_reply[1])
			var msgid uint32
			switch val.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				msgid = uint32(val.Int())
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				msgid = uint32(val.Uint())
			default:
				// error out here as well since we can't figure out where to send the response
				log.Printf("Can't parse msgId from %v, skipping reply\n", msgpack_reply[1])
				continue
			}			
			
			// The error, if not nil, expect a string
			if msgpack_reply[2] != nil {
				if errStr, ok := msgpack_reply[2].(string); ok {
					err = errors.New(errStr)
				} else {
					log.Printf("Error not nil, but not string: %v\n", msgpack_reply[2])
				}
			} else {
				err = nil
			}
			// Send the reply to the receiver for routing back to the sender
			c.replyChanInternal <- ClientResponse{msgid, err, msgpack_reply[3]}
		} else {
			log.Printf("Bad reply from server: %v\n", v)
		}
	}
	log.Println("RPC socket reader shutting down")
}

type ClientClosedError struct {
	Cause error
}
func (cce *ClientClosedError) Error() string {
	return cce.Cause.Error()
}
