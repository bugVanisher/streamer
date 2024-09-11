package slice

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Options struct {
	SID                    string
	HandlerName            string
	ConnectedTimestamp     time.Time
	AfterReadSlicePacket   func(*Packet) error
	AfterWriteSlicePacket  func(*Packet) error
	AfterReadSliceHeaders  func([]Packet) error
	AfterWriteSliceHeaders func([]Packet) error
}

type Option func(*Options)

// WithSID 设置Options的sid选项
func WithSID(id string) Option {
	return func(opts *Options) {
		opts.SID = id
	}
}

// WithHandlerName 设置Options的HandlerName选项
func WithHandlerName(name string) Option {
	return func(opts *Options) {
		opts.HandlerName = name
	}
}

// WithConnectedTimestamp 设置Options的ConnectedTimestamp选项
func WithConnectedTimestamp(t time.Time) Option {
	return func(opts *Options) {
		opts.ConnectedTimestamp = t
	}
}

func WithAfterReadSlicePacket(f func(*Packet) error) Option {
	return func(opts *Options) {
		opts.AfterReadSlicePacket = f
	}
}

func WithAfterWriteSlicePacket(f func(*Packet) error) Option {
	return func(opts *Options) {
		opts.AfterWriteSlicePacket = f
	}
}

func WithAfterReadSliceHeaders(f func([]Packet) error) Option {
	return func(opts *Options) {
		opts.AfterReadSliceHeaders = f
	}
}

func WithAfterWriteSliceHeaders(f func([]Packet) error) Option {
	return func(opts *Options) {
		opts.AfterWriteSliceHeaders = f
	}
}

// Transport 从高层次封装了slice传输
type Transport struct {
	opts            *Options
	labels          map[string]string
	firstPacketSent bool
	lastSendTs      time.Time
}

// NewTransport 创建Transport实例
func NewTransport(opt ...Option) *Transport {
	t := &Transport{}
	opts := &Options{}
	for _, o := range opt {
		o(opts)
	}
	t.opts = opts
	t.labels = make(map[string]string)
	t.labels["handler"] = t.opts.HandlerName
	t.lastSendTs = time.Now()
	return t
}

// CopyAV ...
func (t *Transport) CopySlice(ctx context.Context, dst Muxer, src Demuxer) error {
	err := t.CopyHeaders(ctx, dst, src)
	if err != nil {
		return err
	}
	cerr := t.CopyPackets(ctx, dst, src)
	if cerr != nil {
		if cerr != io.EOF {
			return cerr
		}
	}
	if contextDone(ctx) {
		return fmt.Errorf("transport is canceled")
	}
	if err = dst.WriteTrailer(); err != nil {
		return err
	}
	return cerr
}

// CopyHeaders ...
func (t *Transport) CopyHeaders(ctx context.Context, dst Muxer, src Demuxer) (err error) {
	if contextDone(ctx) {
		return fmt.Errorf("slice transport is canceled")
	}
	header, err := src.Headers()
	if err != nil {
		return
	}
	if t.opts.AfterReadSliceHeaders != nil {
		if err = t.opts.AfterReadSliceHeaders(header); err != nil {
			return err
		}
	}
	if err = dst.WriteHeader(header); err != nil {
		return
	}
	if t.opts.AfterWriteSliceHeaders != nil {
		if err = t.opts.AfterWriteSliceHeaders(header); err != nil {
			return err
		}
	}
	return nil
}

// CopyPackets ...
func (t *Transport) CopyPackets(ctx context.Context, dst Muxer, src Demuxer) (err error) {
	for {
		t.lastSendTs = time.Now()
		if contextDone(ctx) {
			return fmt.Errorf("slice transport is canceled")
		}
		var pkt Packet
		if pkt, err = src.ReadPacket(); err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		if pkt.HeaderChanged {
			if err = t.CopyHeaders(ctx, dst, src); err != nil {
				return
			}
			// 只更新header，不写入packet
			if pkt.IsHeader() {
				continue
			}
		}
		if t.opts.AfterReadSlicePacket != nil {
			if err = t.opts.AfterReadSlicePacket(&pkt); err != nil {
				return err
			}
		}

		if err = dst.WritePacket(pkt); err != nil {
			return
		}
		if t.opts.AfterWriteSlicePacket != nil {
			if err = t.opts.AfterWriteSlicePacket(&pkt); err != nil {
				return err
			}
		}
		if !t.firstPacketSent {
			t.firstPacketSent = true
		}
	}
	return
}

func contextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}
