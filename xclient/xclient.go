package xclient

import (
	"context"
	"github.com/jiaxwu/rpc"
	"reflect"
	"sync"
)

type XClient struct {
	d       Discovery
	mode    SelectMode
	opt     *rpc.Option
	mu      sync.Mutex
	clients map[string]*rpc.Client
}

func NewXClient(d Discovery, mode SelectMode, opt *rpc.Option) *XClient {
	return &XClient{
		d:       d,
		mode:    mode,
		opt:     opt,
		clients: make(map[string]*rpc.Client),
	}
}

func (c *XClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for addr, client := range c.clients {
		_ = client.Close()
		delete(c.clients, addr)
	}
	return nil
}

func (c *XClient) dial(rpcAddr string) (*rpc.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	client, ok := c.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(c.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = rpc.XDial(rpcAddr, c.opt)
		if err != nil {
			return nil, err
		}
		c.clients[rpcAddr] = client
	}
	return client, nil
}

func (c *XClient) call(ctx context.Context, rpcAddr string, serviceMethod string, args, reply any) error {
	client, err := c.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, args, reply)
}

func (c *XClient) Call(ctx context.Context, serviceMethod string, args, reply any) error {
	rpcAddr, err := c.d.Get(c.mode)
	if err != nil {
		return err
	}
	return c.call(ctx, rpcAddr, serviceMethod, args, reply)
}

func (c *XClient) Go(serviceMethod string, args, reply any, done chan *rpc.Call) *rpc.Call {
	rpcAddr, err := c.d.Get(c.mode)
	if err != nil {
		return &rpc.Call{
			Error:         err,
			ServiceMethod: serviceMethod,
			Args:          args,
			Reply:         reply,
			Done:          done,
		}
	}
	client, err := c.dial(rpcAddr)
	if err != nil {
		return &rpc.Call{
			Error:         err,
			ServiceMethod: serviceMethod,
			Args:          args,
			Reply:         reply,
			Done:          done,
		}
	}
	return client.Go(serviceMethod, args, reply, done)
}

func (c *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply any) error {
	rpcAddrs, err := c.d.GetAll()
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var e error
	replyDone := reply == nil
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	for _, rpcAddr := range rpcAddrs {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply any
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := c.call(ctx, rpcAddr, serviceMethod, args, clonedReply)
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel()
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(rpcAddr)
	}
	wg.Wait()
	return e
}
