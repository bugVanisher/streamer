package av

import (
	"context"
	"fmt"
	"io"
	"time"
)

type Options struct {
	SID                string
	HandlerName        string
	ConnectedTimestamp time.Time
	AfterReadPacket    func(*Packet) error
	AfterWritePacket   func(*Packet) error
	AfterReadHeaders   func([]CodecData) error
	AfterWriteHeaders  func([]CodecData) error
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

func WithAfterReadPacket(f func(*Packet) error) Option {
	return func(opts *Options) {
		opts.AfterReadPacket = f
	}
}

func WithAfterWritePacket(f func(*Packet) error) Option {
	return func(opts *Options) {
		opts.AfterWritePacket = f
	}
}

func WithAfterReadHeaders(f func([]CodecData) error) Option {
	return func(opts *Options) {
		opts.AfterReadHeaders = f
	}
}

func WithAfterWriteHeaders(f func([]CodecData) error) Option {
	return func(opts *Options) {
		opts.AfterWriteHeaders = f
	}
}

// Transport 从高层次封装了AV传输
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
func (t *Transport) CopyAV(ctx context.Context, dst Muxer, src Demuxer) error {
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
		return fmt.Errorf("transport is canceled")
	}
	var headers []CodecData
	if headers, err = src.Streams(); err != nil {
		return
	}
	if t.opts.AfterReadHeaders != nil {
		if err = t.opts.AfterReadHeaders(headers); err != nil {
			return err
		}
	}
	if err = dst.WriteHeader(headers); err != nil {
		return
	}
	if t.opts.AfterWriteHeaders != nil {
		if err = t.opts.AfterWriteHeaders(headers); err != nil {
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
			return fmt.Errorf("transport is canceled")
		}
		var pkt Packet
		if pkt, err = src.ReadPacket(); err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		if t.opts.AfterReadPacket != nil {
			if err = t.opts.AfterReadPacket(&pkt); err != nil {
				return err
			}
		}
		if pkt.HeaderChanged {
			if err = t.CopyHeaders(ctx, dst, src); err != nil {
				return
			}
			// 如果是seq header 或者 meta data，只更新header，不写入packet. todo
			if pkt.IsSequenceHeader() || pkt.IsScriptData() {
				continue
			}
		}
		if pkt.Drop {
			continue
		}
		if err = dst.WritePacket(pkt); err != nil {
			return
		}
		if t.opts.AfterWritePacket != nil {
			if err = t.opts.AfterWritePacket(&pkt); err != nil {
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
