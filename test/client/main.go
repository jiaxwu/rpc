package main

import (
	"fmt"
	"github.com/jiaxwu/rpc"
	"log"
	"net"
	"sync"
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
	client, err := rpc.Dial("tcp", l.Addr().String(), nil)
	if err != nil {
		log.Fatal("client: dial error:", err)
	}
	defer func() {
		_ = client.Close()
	}()
	time.Sleep(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("rpc req %d", i)
			var reply string
			if err := client.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply: ", reply)
		}(i)
	}
	wg.Wait()
}
