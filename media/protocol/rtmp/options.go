package rtmp

import "time"

var DefaultOptions = NewOptions()

// rtmp连接的参数选项
type Options struct {
	DialTimeout      time.Duration
	ReadWriteTimeout time.Duration
	ReadBufferSize   int // 单位: 字节
	WriteBufferSize  int // 单位: 字节
	ChunkSize        int // 单位：字节
	RoleID           string
	EnableDebug      bool
	IsServer         bool
	VideoHeaderCheck bool
	Hook             Hook
	TcURL            string
}

// rtmp连接的参数选项设置函数
type Option func(*Options)

// NewOptions 创建rtmp连接选项
func NewOptions() Options {
	return Options{
		ReadWriteTimeout: time.Second * 10,
		DialTimeout:      time.Second * 3,
		ReadBufferSize:   4 * 1024,
		WriteBufferSize:  4 * 1024,
		ChunkSize:        9 * 1024 * 1024,
		IsServer:         true,
		EnableDebug:      false,
		VideoHeaderCheck: true,
	}
}

// WithDialTimeout 建立连接的超时时间
func WithDialTimeout(dialTimeout time.Duration) Option {
	return func(opts *Options) {
		opts.DialTimeout = dialTimeout
	}
}

// WithReadWriteTimeout 设置rtmp连接的读超时时间
func WithReadWriteTimeout(timeout time.Duration) Option {
	return func(opts *Options) {
		opts.ReadWriteTimeout = timeout
	}
}

// WithReadBufferSize 设置rtmp连接读缓存的大小
func WithReadBufferSize(size int) Option {
	return func(opts *Options) {
		opts.ReadBufferSize = size
	}
}

// WithWriteBufferSize 设置rtmp连接写缓存的大小
func WithWriteBufferSize(size int) Option {
	return func(opts *Options) {
		opts.WriteBufferSize = size
	}
}

// WithChunkSize 设置rtmp的ChunkSize
func WithChunkSize(size int) Option {
	return func(opts *Options) {
		opts.ChunkSize = size
	}
}

// WithServerHook 设置rtmp服务端的hook
func WithServerHook(hook Hook) Option {
	return func(opts *Options) {
		opts.Hook = hook
	}
}

// WithEnableDebug 设置debug开关
func WithEnableDebug(enable bool) Option {
	return func(opts *Options) {
		opts.EnableDebug = enable
	}
}

// WithVideoHeaderCheck 设置视频头部校验开关
func WithVideoHeaderCheck(check bool) Option {
	return func(opts *Options) {
		opts.VideoHeaderCheck = check
	}
}

// WithRoleID 设置RoleID
func WithRoleID(role string) Option {
	return func(opts *Options) {
		opts.RoleID = role
	}
}

// WithTcURL 设置tcUrl
func WithTcURL(u string) Option {
	return func(opts *Options) {
		opts.TcURL = u
	}
}
