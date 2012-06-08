MsgPack library for Go.
=======================

Implements:
>  [http://wiki.msgpack.org/display/MSGPACK/Format+specification]

To install:
>  go get github.com/ugorji/go-msgpack

It provides features similar to encoding packages in the standard library (ie json, xml, gob, etc).

Supports:
  * Standard Marshal/Unmarshal interface.
  * Support for all exported fields (including anonymous fields)
  * Standard field renaming via tags
  * Encoding from any value (struct, slice, map, primitives, pointers, interface{}, etc)
  * Decoding into pointer to any non-nil value (struct, slice, map, int, float32, bool, string, etc)
  * Decoding into a nil interface{} 
  * Handles time.Time transparently (stores time as 2 element array: seconds since epoch and nanosecond offset)
  * Provides a Server and Client Codec so msgpack can be used as communication protocol for net/rpc.
    * Also includes an option for msgpack-rpc: http://wiki.msgpack.org/display/MSGPACK/RPC+specification

Usage
-----

<pre>

    dec = msgpack.NewDecoder(r, nil)  
    err = dec.Decode(&v)  
    
    enc = msgpack.NewEncoder(w)  
    err = enc.Encode(v)  
    
    //methods below are convenience methods over functions above.  
    data, err = msgpack.Marshal(v)  
    err = msgpack.Unmarshal(data, &v, nil)  
    
    //RPC Server
    conn, err := listener.Accept()
    rpcCodec := msgpack.NewRPCServerCodec(conn, nil)
    rpc.ServeCodec(rpcCodec)

    //RPC Communication (client side)
    conn, err = net.Dial("tcp", "localhost:5555")  
    rpcCodec := msgpack.NewRPCClientCodec(conn, nil)  
    client := rpc.NewClientWithCodec(rpcCodec)  

</pre>
