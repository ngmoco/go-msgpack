/*
MsgPack library for Go.

Implements:
  http://wiki.msgpack.org/display/MSGPACK/Format+specification

It provides features similar to encoding packages in the standard library (ie json, xml, gob, etc).

Supports:
  - Standard Marshal/Unmarshal interface.
  - Standard field renaming via tags
  - Encoding from any value (struct, slice, map, primitives, pointers, interface{}, etc)
  - Decoding into pointer to any non-nil value (struct, slice, map, int, float32, bool, string, etc)
  - Decoding into a nil interface{} 
  - Handles time.Time transparently 
  - Provides a Server and Client Codec so msgpack can be used as communication protocol for net/rpc.
    Also includes an option for msgpack-rpc: http://wiki.msgpack.org/display/MSGPACK/RPC+specification

Usage

  dec = msgpack.NewDecoder(r, nil)
  err = dec.Decode(&v) 
  
  enc = msgpack.NewEncoder(w)
  err = enc.Encode(v) 
  
  //methods below are convenience methods over functions above.
  data, err = msgpack.Marshal(v) 
  err = msgpack.Unmarshal(data, &v, nil)
  
  //RPC Communication
  conn, err = net.Dial("tcp", "localhost:5555")
  rpcCodec := msgpack.NewRPCCodec(conn, nil, true)
  client := rpc.NewClientWithCodec(rpcCodec)
 
*/
package msgpack


