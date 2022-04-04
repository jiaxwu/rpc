package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jiaxwu/rpc/codec"
	"log"
	"net"
	"sync"
	"time"
)

// Call 代表一次RPC调度
type Call struct {
	Seq           int
	ServiceMethod string
	Args          any
	Reply         any
	Error         error
	Done          chan *Call // 结束调用时，通过这个通道通知调用方
}

// 调用结束
func (c *Call) done() {
	c.Done <- c
}

// Client RPC客户端
type Client struct {
	cc       codec.Codec
	opt      *Option
	sending  sync.Mutex
	header   codec.Header // 复用
	mu       sync.Mutex
	seq      int
	pending  map[int]*Call // 挂起的请求
	closing  bool          // 客户端主动关闭
	shutdown bool          // 服务器主动关闭
}

var ErrShutdown = errors.New("connection is shutdown")

// Close 关闭客户端
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing {
		return ErrShutdown
	}
	c.closing = true
	return c.cc.Close()
}

// IsAvailable 客户端是否还能正常工作
func (c *Client) IsAvailable() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closing && !c.shutdown
}

// 注册Call
func (c *Client) registerCall(call *Call) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closing || c.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = c.seq
	c.seq++
	c.pending[call.Seq] = call
	return call.Seq, nil
}

// 移除Call
func (c *Client) removeCall(seq int) *Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	call := c.pending[seq]
	delete(c.pending, seq)
	return call
}

// 终止Calls
func (c *Client) terminateCalls(err error) {
	c.sending.Lock()
	defer c.sending.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.shutdown = true
	for _, call := range c.pending {
		call.Error = err
		call.done()
	}
}

// 接收响应
func (c *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		// header读取出错，则无法继续执行
		if err = c.cc.ReadHeader(&h); err != nil {
			break
		}
		call := c.removeCall(h.Seq)
		switch {
		// call已经被移除了，比如超时
		case call == nil:
			err = c.cc.ReadBody(nil)
		// 请求出错
		case h.Error != "":
			call.Error = errors.New(h.Error)
			err = c.cc.ReadBody(nil)
			call.done()
		// 正常的读取响应
		default:
			if err = c.cc.ReadBody(call.Reply); err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	c.terminateCalls(err)
}

// NewClient 创建客户端
func NewClient(conn net.Conn, opt *Option) (*Client, error) {
	if opt == nil {
		opt = DefaultOption
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.CodecType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error:", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *Option) *Client {
	client := &Client{
		cc:      cc,
		opt:     opt,
		seq:     1,
		pending: make(map[int]*Call),
	}
	go client.receive()
	return client
}

type clientResult struct {
	client *Client
	err    error
}

// Dial 连接RPC服务器
func Dial(network, address string, opt *Option) (c *Client, err error) {
	if opt == nil {
		opt = DefaultOption
	} else {
		opt.MagicNumber = MagicNumber
		if opt.CodecType == "" {
			opt.CodecType = DefaultOption.CodecType
		}
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectTimeout)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	// NewClient超时处理
	ch := make(chan clientResult)
	go func() {
		client, err := NewClient(conn, opt)
		ch <- clientResult{
			client: client,
			err:    err,
		}
	}()
	// 如果超时时间为0，表示可以一直等待
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	// 否则等待直到超时
	select {
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

func (c *Client) send(call *Call) {
	c.sending.Lock()
	defer c.sending.Unlock()

	// 注册call
	seq, err := c.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	c.header.ServiceMethod = call.ServiceMethod
	c.header.Seq = seq
	c.header.Error = ""
	if err := c.cc.Write(&c.header, call.Args); err != nil {
		call := c.removeCall(seq)
		// 这里主要是可能call被意外的移除
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go 这个是异步的调用
func (c *Client) Go(serviceMethod string, args, reply any, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		// 这种情况下，会导致done()阻塞，不应该继续执行
		log.Panic("rpc: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	c.send(call)
	return call
}

// Call 同步调用
func (c *Client) Call(ctx context.Context, serviceMethod string, args, reply any) error {
	call := c.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		c.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}
