package codec

import "io"

// Header 请求头
type Header struct {
	// 请求方法，格式为：服务名.方法名
	ServiceMethod string
	// 序列号，区分不同请求
	Seq int
	// 错误信息
	Error string
}

type Codec interface {
	io.Closer
	ReadHeader(header *Header) error
	ReadBody(body any) error
	Write(header *Header, body any) error
}

// NewCodecFunc 创建编码器函数
type NewCodecFunc func(conn io.ReadWriteCloser) Codec

// Type 编码器类型
type Type string

const (
	GobType  Type = "application/gob"
	JSONType Type = "application/json"
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
