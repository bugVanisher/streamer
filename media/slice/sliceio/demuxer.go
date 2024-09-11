package sliceio

import (
	"bufio"
	"bytes"
	"fmt"
	"io"

	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/container/flv"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"github.com/bugVanisher/streamer/media/slice"
	"github.com/bugVanisher/streamer/utils"
)

var MaxProbePacketCount = 20

type Demuxer struct {
	r                              io.ReadCloser
	bufr                           *bufio.Reader
	b                              []byte
	stage                          int
	headers                        []slice.Packet
	cachedSice                     []slice.Packet
	hasVideoHeader, hasAudioHeader bool
	avcHeaderIdx, aacHeaderIdx     int
}

func NewDemuxer(r io.ReadCloser) *Demuxer {
	return &Demuxer{
		r:    r,
		bufr: bufio.NewReaderSize(r, 4*1024),
		b:    make([]byte, 256),
	}
}

func (self *Demuxer) prepare() (err error) {
	//有avcheader和aacheader return TRUE；或者 slice cache得到MaxProbePacketCount true
	for self.stage < MaxProbePacketCount {

		if self.hasAudioHeader && self.hasVideoHeader {
			return
		}

		var pkt slice.Packet
		if pkt, err = ReadSlice(self.bufr, self.b); err != nil {
			return
		}

		if pkt.SliceType == slice.SLICE_TYPE_FLV_HEADER {
			idx := len(self.headers)
			self.headers = append(self.headers, pkt)

			if pkt.SliceId == slice.SLICE_ID_AVC_HEADER {
				self.hasVideoHeader = true
				self.avcHeaderIdx = idx
			} else if pkt.SliceId == slice.SLICE_ID_AAC_HEADER {
				self.hasAudioHeader = true
				self.aacHeaderIdx = idx
			}
		} else {
			self.cachedSice = append(self.cachedSice, pkt)
			self.stage++
		}
	}
	return
}

func (self *Demuxer) HeaderChanged(pkt slice.Packet) bool {
	var headerChanged bool
	if pkt.SliceId == slice.SLICE_ID_AVC_HEADER {
		if self.hasVideoHeader && !bytes.Equal(self.headers[self.avcHeaderIdx].Data, pkt.Data) {
			headerChanged = true
			self.headers[self.avcHeaderIdx] = pkt
		} else if !self.hasVideoHeader {
			self.hasVideoHeader = true
			headerChanged = true
			self.avcHeaderIdx = len(self.headers)
			self.headers = append(self.headers, pkt)
		}
	} else if pkt.SliceId == slice.SLICE_ID_AAC_HEADER {
		if self.hasAudioHeader && !bytes.Equal(self.headers[self.aacHeaderIdx].Data, pkt.Data) {
			headerChanged = true
			self.headers[self.aacHeaderIdx] = pkt
		} else if !self.hasAudioHeader {
			self.hasAudioHeader = true
			headerChanged = true
			self.aacHeaderIdx = len(self.headers)
			self.headers = append(self.headers, pkt)
		}
	}
	return headerChanged
}

func (self *Demuxer) Headers() (header []slice.Packet, err error) {
	if err := self.prepare(); err != nil {
		return nil, err
	}
	header = self.headers
	return
}

func (self *Demuxer) ReadPacket() (pkt slice.Packet, err error) {
	if err = self.prepare(); err != nil {
		return
	}

	if !self.sliceEmpty() {
		pkt = self.popSlice()
		return
	}

	for {
		if pkt, err = ReadSlice(self.bufr, self.b); err != nil {
			return
		}

		if pkt.SliceType == slice.SLICE_TYPE_FLV_HEADER {
			if !self.HeaderChanged(pkt) {
				continue
			} else {
				pkt.HeaderChanged = true
				return
			}
		}
		return
	}

	return
}

func (self *Demuxer) sliceEmpty() bool {
	return len(self.cachedSice) == 0
}

func (self *Demuxer) popSlice() slice.Packet {
	pkt := self.cachedSice[0]
	self.cachedSice = self.cachedSice[1:]
	return pkt
}

func (self *Demuxer) Close() error {
	return self.r.Close()
}

func ReadSlice(r io.Reader, b []byte) (pkt slice.Packet, err error) {
	var readlen int
	if readlen, err = io.ReadFull(r, b[:slice.KSliceHeaderSize]); err != nil {
		err = fmt.Errorf("io.ReadFull sliceHeader readlen:%d, err:%s", readlen, err.Error())
		return
	}
	var datalen uint16
	if pkt, datalen, err = slice.ParseSliceHeader(b); err != nil {
		return
	}
	pkt.Data = make([]byte, datalen)
	copy(pkt.Data, b[0:slice.KSliceHeaderSize])
	if readlen, err = io.ReadFull(r, pkt.Data[slice.KSliceHeaderSize:]); err != nil {
		err = fmt.Errorf("io.ReadFull sliceID:%d,sliceSize:%d,readBufLen:%d,readlen:%d, err:%s",
			pkt.SliceId, pkt.Size, datalen-slice.KSliceHeaderSize, readlen, err.Error())
		return
	}

	// flv 头部不需要解析DTS
	if pkt.SliceType == slice.SLICE_TYPE_FLV_HEADER || pkt.SliceType == slice.SLICE_TYPE_SCRIPT_DATA {
		return
	}

	// 有设置Extend
	var extendSize uint16
	if pkt.ExtendFlag == 1 {
		extendSize = utils.BytesToUint16(pkt.Data[slice.KSliceHeaderSize : slice.KSliceHeaderSize+2])
		pkt.Extend = slice.NewExtend()
		pkt.Extend.Decode(pkt.Data[slice.KSliceHeaderSize : slice.KSliceHeaderSize+extendSize])
	}
	// parse frame dts from audio&video
	if pkt.PosFlag == slice.SLICE_POSFLAG_START || pkt.PosFlag == slice.SLICE_POSFLAG_STARTEND {
		var tagSize int
		sliceHeaderSize := slice.KSliceHeaderSize + int(extendSize)
		_, pkt.FrameDts, tagSize, err = flvio.ParseTagHeader(pkt.Data[sliceHeaderSize:])
		if err != nil {
			err = fmt.Errorf("slice ParseTagHeader err:%s", err.Error())
		}
		// skip avc header
		if pkt.FrameDts == 0 && sliceHeaderSize+tagSize+4+flvio.TagHeaderLength*2 < int(datalen) {
			_, pkt.FrameDts, tagSize, err = flvio.ParseTagHeader(pkt.Data[sliceHeaderSize+flvio.TagHeaderLength+tagSize+4:])
			if err != nil {
				err = fmt.Errorf("slice ParseTagHeader err2:%s", err.Error())
			}
		}
	}
	return
}

func AVHeaderToSliceHeader(streams []av.CodecData) []slice.Packet {
	var err error
	tmpbuf := make([]byte, 256)
	var flags uint8
	for _, stream := range streams {
		if stream.Type().IsVideo() {
			flags |= flvio.FILE_HAS_VIDEO
		} else if stream.Type().IsAudio() {
			flags |= flvio.FILE_HAS_AUDIO
		}
	}
	n := flvio.FillFileHeader(tmpbuf, flags)
	flvFileHeader := make([]byte, n)
	copy(flvFileHeader, tmpbuf[0:n])

	// make sliceID 1/2
	headers := make([]slice.Packet, 0, 2)
	for _, stream := range streams {
		var tag flvio.Tag
		var ok bool
		if tag, ok, err = flv.CodecDataToTag(stream); err != nil {
			return nil
		}

		if ok {
			var buf bytes.Buffer
			if err = flvio.WriteTag(&buf, tag, 0, tmpbuf); err != nil {
				return nil
			}
			// flvheader + avcheader
			headerTag := make([]byte, len(flvFileHeader)+buf.Len())
			copy(headerTag, flvFileHeader)
			copy(headerTag[len(flvFileHeader):], buf.Bytes())
			sliceHeader := slice.GenerateHeaderSlice(headerTag, tag)
			headers = append(headers, sliceHeader)
		}
	}
	return headers
}
