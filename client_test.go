package rpc

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

type Bar int

func (b Bar) Timeout(argv int, reply *int) error {
	time.Sleep(time.Second * 2)
	return nil
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	// 启动服务器
	var b Bar
	_ = Register(&b)
	l, _ := net.Listen("tcp", ":0")
	go func() {
		Accept(l)
	}()

	time.Sleep(time.Second)
	// 客户端测试
	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", l.Addr().String(), nil)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Bar.Timeout", 1, &reply)
		if err != nil {
			fmt.Println(err)
		}
	})
	// 服务器测试
	t.Run("server handle timeout", func(t *testing.T) {
		client, _ := Dial("tcp", l.Addr().String(), &Option{
			HandleTimeout: time.Second,
		})
		var reply int
		err := client.Call(context.Background(), "Bar.Timeout", 1, &reply)
		if err != nil {
			fmt.Println(err)
		}
	})
}
