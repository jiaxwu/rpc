package main

import (
	"encoding/json"
	"fmt"
	"github.com/jiaxwu/rpc"
	"github.com/jiaxwu/rpc/codec"
	"log"
	"net"
	"time"
)

func main() {
	// 启动服务器
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	go rpc.Accept(l)

	// 客户端
	conn, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		log.Fatal("client: dial error:", err)
	}
	defer func() {
		_ = conn.Close()
	}()
	time.Sleep(time.Second)
	if err := json.NewEncoder(conn).Encode(rpc.DefaultOption); err != nil {
		log.Fatal("client: send option error:", err)
	}
	cc := codec.NewGobCodec(conn)
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           i,
		}
		if err := cc.Write(h, fmt.Sprintf("rpc req %d", h.Seq)); err != nil {
			log.Fatal("client: send request error:", err)
		}
		if err := cc.ReadHeader(h); err != nil {
			log.Fatal("client: read header error:", err)
		}
		var reply string
		if err := cc.ReadBody(&reply); err != nil {
			log.Fatal("client: read reply error:", err)
		}
		log.Printf("client: reply %s\n", reply)
	}
}
