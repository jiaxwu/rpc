package rpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jiaxwu/rpc/codec"
	"io"
	"log"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"
)

// MagicNumber 魔数
const MagicNumber = 0x3bef5c

// Option 协商信息，协商协议类型
//| Option{MagicNumber: xxx, CodecType: xxx} | Header{ServiceMethod ...} | Body interface{} |
//| <------      固定 JSON 编码      ------>  | <-------   编码方式由 CodeType 决定   ------->|
type Option struct {
	MagicNumber    int
	CodecType      codec.Type
	ConnectTimeout time.Duration // 连接超时
	HandleTimeout  time.Duration // 处理超时
}

// DefaultOption 默认协商信息
var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.GobType,
	ConnectTimeout: time.Second * 10,
}

// Server RPC服务器
type Server struct {
	// 服务集合
	serviceMap sync.Map
}

// NewServer 创建新的服务器
func NewServer() *Server {
	return &Server{}
}

// DefaultServer 默认服务器实例
var DefaultServer = NewServer()

// Accept 接受请求并处理
func (s *Server) Accept(lis net.Listener) {
	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go s.ServeConn(conn)
	}
}

// Accept 使用默认服务器实例去接受请求并处理
func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

// ServeConn 处理一个连接的请求
func (s *Server) ServeConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	// 解析选项
	var opt Option
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: decode option error: ", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", opt.CodecType)
		return
	}
	// 根据选择的编码方式处理请求
	s.serveCodec(f(conn), &opt)
}

// 错误时响应体的占位符
var invalidRequest = struct{}{}

// 请求
type request struct {
	h      *codec.Header
	argv   reflect.Value // 请求参数
	replyv reflect.Value // 响应参数
	mType  *methodType   // 请求的处理方法
	svc    *service      // 请求的处理服务
}

// 根据编码方式处理请求
func (s *Server) serveCodec(cc codec.Codec, opt *Option) {
	defer func() {
		_ = cc.Close()
	}()
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		// 读取请求
		req, err := s.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		// 处理请求
		go s.handleRequest(cc, req, sending, wg, opt.HandleTimeout)
	}
	wg.Wait()
}

// 读取请求头
func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// 读取请求
func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	// 读取请求头
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mType, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mType.newArgv()
	req.replyv = req.mType.newReplyv()

	// 确保请求参数是一个指针，因为ReadBody()必须一个指针类型参数
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Pointer {
		argvi = req.argv.Addr().Interface()
	}
	// 读取请求体
	if err := cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read argv error:", err)
		return req, err
	}
	return req, nil
}

// 发送响应
func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body any, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// 处理请求
func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.call(req.mType, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()

	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: connect timeout: expect within %s", timeout)
		s.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}
}

// Register 注册服务
func (s *Server) Register(rcvr any) error {
	svc := newService(rcvr)
	if _, loaded := s.serviceMap.LoadOrStore(svc.name, svc); loaded {
		return fmt.Errorf("rpc server: service already defined: %s", svc.name)
	}
	return nil
}

// Register 默认服务器注册服务
func Register(rcvr any) error {
	return DefaultServer.Register(rcvr)
}

func (s *Server) findService(serviceMethod string) (*service, *methodType, error) {
	serviceName, methodName, found := strings.Cut(serviceMethod, ".")
	if !found {
		return nil, nil, fmt.Errorf("rpc server: service/method request ill-formed: %s", serviceMethod)
	}
	svci, ok := s.serviceMap.Load(serviceName)
	if !ok {
		return nil, nil, fmt.Errorf("rpc server: can't find service %s", serviceName)
	}
	svc := svci.(*service)
	mType := svc.method[methodName]
	if mType == nil {
		return nil, nil, errors.New("rpc server: can't find method " + methodName)
	}
	return svc, mType, nil
}
