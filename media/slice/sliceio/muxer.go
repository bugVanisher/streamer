package sliceio

import (
	"bufio"
	"fmt"
	"io"

	"github.com/bugVanisher/streamer/media/slice"
	"github.com/bugVanisher/streamer/utils/bits/pio"
)

type Muxer struct {
	bufw               writeFlusher
	header             []slice.Packet
	flvHeaderSent      bool
	lastSendPacketType uint8
}

type writeFlusher interface {
	io.Writer
	Flush() error
}

func NewMuxerWriteFlusher(w writeFlusher) *Muxer {
	return &Muxer{
		bufw: w,
	}
}

func NewMuxer(w io.Writer) *Muxer {
	return NewMuxerWriteFlusher(bufio.NewWriterSize(w, pio.RecommendBufioSize))
}

func (self *Muxer) WriteHeader(headers []slice.Packet) (err error) {
	for _, header := range headers {
		if _, err = self.bufw.Write(header.Data); err != nil {
			return
		}
	}
	return
}

func (self *Muxer) WritePacket(pkt slice.Packet) (err error) {
	//帧类型发生变换时立马发送上一个帧数据，同一个帧的切片数据一块发送
	if pkt.SliceType != self.lastSendPacketType {
		if err = self.bufw.Flush(); err != nil {
			err = fmt.Errorf("flv.Muxer Flush error: %v", err)
			return
		}
		self.lastSendPacketType = pkt.SliceType
	}

	if _, err = self.bufw.Write(pkt.Data); err != nil {
		return
	}

	return
}

func (self *Muxer) WriteTrailer() (err error) {
	if err = self.bufw.Flush(); err != nil {
		return
	}
	return
}

func (self *Muxer) Close() (err error) {
	// TODO
	return nil
}
