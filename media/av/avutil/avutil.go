package avutil

import (
	"bytes"
	"fmt"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"io"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/codec/aacparser"
	"github.com/bugVanisher/streamer/media/codec/h264parser"
)

type HandlerDemuxer struct {
	av.Demuxer
	r io.ReadCloser
}

func (self *HandlerDemuxer) Close() error {
	return self.r.Close()
}

type HandlerMuxer struct {
	av.Muxer
	w     io.WriteCloser
	stage int
}

func (self *HandlerMuxer) WriteHeader(streams []av.CodecData) (err error) {
	if self.stage == 0 {
		if err = self.Muxer.WriteHeader(streams); err != nil {
			return
		}
		self.stage++
	}
	return
}

func (self *HandlerMuxer) WriteTrailer() (err error) {
	if self.stage == 1 {
		self.stage++
		if err = self.Muxer.WriteTrailer(); err != nil {
			return
		}
	}
	return
}

func (self *HandlerMuxer) Close() (err error) {
	if err = self.WriteTrailer(); err != nil {
		return
	}
	return self.w.Close()
}

type RegisterHandler struct {
	Ext           string
	ReaderDemuxer func(io.Reader) av.Demuxer
	WriterMuxer   func(io.Writer) av.Muxer
	UrlMuxer      func(string) (bool, av.MuxCloser, error)
	UrlDemuxer    func(string) (bool, av.DemuxCloser, error)
	UrlReader     func(string) (bool, io.ReadCloser, error)
	Probe         func([]byte) bool
	AudioEncoder  func(av.CodecType) (av.AudioEncoder, error)
	AudioDecoder  func(av.AudioCodecData) (av.AudioDecoder, error)
	ServerDemuxer func(string) (bool, av.DemuxCloser, error)
	ServerMuxer   func(string) (bool, av.MuxCloser, error)
	CodecTypes    []av.CodecType
}

type Handlers struct {
	handlers []RegisterHandler
}

func (self *Handlers) Add(fn func(*RegisterHandler)) {
	handler := &RegisterHandler{}
	fn(handler)
	self.handlers = append(self.handlers, *handler)
}

func (self *Handlers) openUrl(u *url.URL, uri string) (r io.ReadCloser, err error) {
	if u != nil && u.Scheme != "" {
		for _, handler := range self.handlers {
			if handler.UrlReader != nil {
				var ok bool
				if ok, r, err = handler.UrlReader(uri); ok {
					return
				}
			}
		}
		err = fmt.Errorf("avutil: openUrl %s failed", uri)
	} else {
		r, err = os.Open(uri)
	}
	return
}

func (self *Handlers) createUrl(u *url.URL, uri string) (w io.WriteCloser, err error) {
	w, err = os.Create(uri)
	return
}

func (self *Handlers) NewAudioEncoder(typ av.CodecType) (enc av.AudioEncoder, err error) {
	for _, handler := range self.handlers {
		if handler.AudioEncoder != nil {
			if enc, _ = handler.AudioEncoder(typ); enc != nil {
				return
			}
		}
	}
	err = fmt.Errorf("avutil: encoder %v not found", typ)
	return
}

func (self *Handlers) NewAudioDecoder(codec av.AudioCodecData) (dec av.AudioDecoder, err error) {
	for _, handler := range self.handlers {
		if handler.AudioDecoder != nil {
			if dec, _ = handler.AudioDecoder(codec); dec != nil {
				return
			}
		}
	}
	err = fmt.Errorf("avutil: decoder %v not found", codec.Type())
	return
}

func (self *Handlers) Open(uri string) (demuxer av.DemuxCloser, err error) {
	listen := false
	if strings.HasPrefix(uri, "listen:") {
		uri = uri[len("listen:"):]
		listen = true
	}

	for _, handler := range self.handlers {
		if listen {
			if handler.ServerDemuxer != nil {
				var ok bool
				if ok, demuxer, err = handler.ServerDemuxer(uri); ok {
					return
				}
			}
		} else {
			if handler.UrlDemuxer != nil {
				var ok bool
				if ok, demuxer, err = handler.UrlDemuxer(uri); ok {
					return
				}
			}
		}
	}

	var r io.ReadCloser
	var ext string
	var u *url.URL
	if u, _ = url.Parse(uri); u != nil && u.Scheme != "" {
		ext = path.Ext(u.Path)
	} else {
		ext = path.Ext(uri)
	}

	if ext != "" {
		for _, handler := range self.handlers {
			if handler.Ext == ext {
				if handler.ReaderDemuxer != nil {
					if r, err = self.openUrl(u, uri); err != nil {
						return
					}
					demuxer = &HandlerDemuxer{
						Demuxer: handler.ReaderDemuxer(r),
						r:       r,
					}
					return
				}
			}
		}
	}

	var probebuf [1024]byte
	if r, err = self.openUrl(u, uri); err != nil {
		return
	}
	if _, err = io.ReadFull(r, probebuf[:]); err != nil {
		return
	}

	for _, handler := range self.handlers {
		if handler.Probe != nil && handler.Probe(probebuf[:]) && handler.ReaderDemuxer != nil {
			var _r io.Reader
			if rs, ok := r.(io.ReadSeeker); ok {
				if _, err = rs.Seek(0, 0); err != nil {
					return
				}
				_r = rs
			} else {
				_r = io.MultiReader(bytes.NewReader(probebuf[:]), r)
			}
			demuxer = &HandlerDemuxer{
				Demuxer: handler.ReaderDemuxer(_r),
				r:       r,
			}
			return
		}
	}

	r.Close()
	err = fmt.Errorf("avutil: open %s failed", uri)
	return
}

func (self *Handlers) Create(uri string) (muxer av.MuxCloser, err error) {
	_, muxer, err = self.FindCreate(uri)
	return
}

func (self *Handlers) FindCreate(uri string) (handler RegisterHandler, muxer av.MuxCloser, err error) {
	listen := false
	if strings.HasPrefix(uri, "listen:") {
		uri = uri[len("listen:"):]
		listen = true
	}

	for _, handler = range self.handlers {
		if listen {
			if handler.ServerMuxer != nil {
				var ok bool
				if ok, muxer, err = handler.ServerMuxer(uri); ok {
					return
				}
			}
		} else {
			if handler.UrlMuxer != nil {
				var ok bool
				if ok, muxer, err = handler.UrlMuxer(uri); ok {
					return
				}
			}
		}
	}

	var ext string
	var u *url.URL
	if u, _ = url.Parse(uri); u != nil && u.Scheme != "" {
		ext = path.Ext(u.Path)
	} else {
		ext = path.Ext(uri)
	}

	if ext != "" {
		for _, handler = range self.handlers {
			if handler.Ext == ext && handler.WriterMuxer != nil {
				var w io.WriteCloser
				if w, err = self.createUrl(u, uri); err != nil {
					return
				}
				muxer = &HandlerMuxer{
					Muxer: handler.WriterMuxer(w),
					w:     w,
				}
				return
			}
		}
	}

	err = fmt.Errorf("avutil: create muxer %s failed", uri)
	return
}

var DefaultHandlers = &Handlers{}

func Open(url string) (demuxer av.DemuxCloser, err error) {
	return DefaultHandlers.Open(url)
}

func Create(url string) (muxer av.MuxCloser, err error) {
	return DefaultHandlers.Create(url)
}

func CopyPackets(dst av.PacketWriter, src av.PacketReader) (err error) {
	for {
		var pkt av.Packet
		if pkt, err = src.ReadPacket(); err != nil {
			if err == io.EOF {
				break
			}
			return
		}
		if err = dst.WritePacket(pkt); err != nil {
			return
		}
	}
	return
}

func CopyFile(dst av.Muxer, src av.Demuxer) (err error) {
	var streams []av.CodecData
	if streams, err = src.Streams(); err != nil {
		return
	}
	if err = dst.WriteHeader(streams); err != nil {
		return
	}
	if err = CopyPackets(dst, src); err != nil {
		if err != io.EOF {
			return
		}
	}
	if err = dst.WriteTrailer(); err != nil {
		return
	}
	return
}

func Equal(c1 []av.CodecData, c2 []av.CodecData) bool {
	if len(c1) != len(c2) {
		return false
	}
	for i, codec := range c1 {
		if codec.Type() != c2[i].Type() {
			return false
		}
		switch codec.Type() {
		case av.H264:
			if eq := bytes.Compare(
				codec.(h264parser.CodecData).AVCDecoderConfRecordBytes(),
				c2[i].(h264parser.CodecData).AVCDecoderConfRecordBytes(),
			); eq != 0 {
				return false
			}
		case av.AAC:
			if eq := bytes.Compare(
				codec.(aacparser.CodecData).MPEG4AudioConfigBytes(),
				c2[i].(aacparser.CodecData).MPEG4AudioConfigBytes(),
			); eq != 0 {
				return false
			}
		}
	}
	return true
}

func ConvertHeader(srchdr []av.CodecData) []av.Header {
	var headers []av.Header
	for _, data := range srchdr {
		switch data.Type() {
		case av.H264:
			c := data.(h264parser.CodecData)
			headers = append(headers, av.Header{
				Type: av.HeaderTypeH264,
				Data: c.SequnceHeaderTag,
			})
		case av.AAC:
			c := data.(aacparser.CodecData)
			headers = append(headers, av.Header{
				Type: av.HeaderTypeAAC,
				Data: c.SequnceHeaderTag,
			})
		}
	}
	return headers
}

func RevertHeader(srchdr []av.Header) []av.CodecData {
	var headers []av.CodecData
	for _, data := range srchdr {
		tag := data.Data.(flvio.Tag)

		switch data.Type {
		case av.HeaderTypeH264:
			videoHdr, _ := h264parser.NewCodecDataFromAVCDecoderConfRecord(tag.Data)
			videoHdr.SequnceHeaderTag = tag
			headers = append(headers, videoHdr)
		case av.HeaderTypeAAC:
			aacHdr, _ := aacparser.NewCodecDataFromMPEG4AudioConfigBytes(tag.Data)
			aacHdr.SequnceHeaderTag = tag
			headers = append(headers, aacHdr)
		}
	}

	return headers
}

func ConvertH264Header(h av.CodecData) (h264parser.CodecData, error) {
	tag := h.(h264parser.CodecData).SequnceHeaderTag.(flvio.Tag)
	return h264parser.NewCodecDataFromAVCDecoderConfRecord(tag.Data)
}

func ConvertAACHeader(h av.CodecData) (aacparser.CodecData, error) {
	tag := h.(aacparser.CodecData).SequnceHeaderTag.(flvio.Tag)
	return aacparser.NewCodecDataFromMPEG4AudioConfigBytes(tag.Data)
}
