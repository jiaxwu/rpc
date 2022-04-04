package main

import (
	"context"
	"github.com/jiaxwu/rpc"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

type Foo int

type Args struct{ Num1, Num2 int }

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func main() {
	ch := make(chan string)

	go func() {
		// 客户端
		client, err := rpc.DialHTTP("tcp", <-ch, nil)
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
				args := &Args{
					Num1: i,
					Num2: i * i,
				}
				var reply int
				if err := client.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
					log.Fatal("call Foo.Sum error:", err)
				}
				log.Printf("%d + %d = %d\n", args.Num1, args.Num2, reply)
			}(i)
		}
		wg.Wait()
	}()

	// 启动服务器
	var foo Foo
	if err := rpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	rpc.HandleHTTP()
	ch <- l.Addr().String()
	_ = http.Serve(l, nil)

}
