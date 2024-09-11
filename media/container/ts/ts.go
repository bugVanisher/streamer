package ts

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"time"

	"github.com/bugVanisher/streamer/media/av"
	aacparser "github.com/bugVanisher/streamer/media/codec/aacparser"
	h264parser "github.com/bugVanisher/streamer/media/codec/h264parser"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"github.com/bugVanisher/streamer/media/container/ts/tsio"
	"github.com/bugVanisher/streamer/utils/bits/pio"
)

var CodecTypes = []av.CodecType{av.H264, av.AAC}

type Stream struct {
	av.CodecData
	muxer   *Muxer
	demuxer *Demuxer

	pid        uint16
	streamId   uint8
	streamType uint8

	tsw *tsio.TSWriter
	idx int

	iskeyframe bool
	pts, dts   time.Duration
	data       []byte
	datalen    int

	config aacparser.MPEG4AudioConfig
	sps    []byte
	pps    []byte
}

type Muxer struct {
	w                        io.Writer
	streams                  []*Stream
	PaddingToMakeCounterCont bool

	psidata []byte
	peshdr  []byte
	tshdr   []byte
	adtshdr []byte
	datav   [][]byte
	nalus   [][]byte

	tswpat, tswpmt *tsio.TSWriter
}

func NewMuxer(w io.Writer) *Muxer {
	return &Muxer{
		w:       w,
		psidata: make([]byte, 188),
		peshdr:  make([]byte, tsio.MaxPESHeaderLength),
		tshdr:   make([]byte, tsio.MaxTSHeaderLength),
		adtshdr: make([]byte, aacparser.ADTSHeaderLength),
		nalus:   make([][]byte, 16),
		datav:   make([][]byte, 16),
		tswpmt:  tsio.NewTSWriter(tsio.PMT_PID),
		tswpat:  tsio.NewTSWriter(tsio.PAT_PID),
	}
}

func (self *Muxer) newStream(codec av.CodecData) (err error) {
	ok := false
	for _, c := range CodecTypes {
		if codec.Type() == c {
			ok = true
			break
		}
	}
	if !ok {
		err = fmt.Errorf("ts: codec type=%s is not supported", codec.Type())
		return
	}

	pid := uint16(len(self.streams) + 0x100)
	stream := &Stream{
		muxer:     self,
		CodecData: codec,
		pid:       pid,
		tsw:       tsio.NewTSWriter(pid),
	}
	self.streams = append(self.streams, stream)
	return
}

func (self *Muxer) writePaddingTSPackets(tsw *tsio.TSWriter) (err error) {
	for tsw.ContinuityCounter&0xf != 0x0 {
		if err = tsw.WritePackets(self.w, self.datav[:0], 0, false, true); err != nil {
			return
		}
	}
	return
}

func (self *Muxer) WriteTrailer() (err error) {
	if self.PaddingToMakeCounterCont {
		for _, stream := range self.streams {
			if err = self.writePaddingTSPackets(stream.tsw); err != nil {
				return
			}
		}
	}
	return
}

func (self *Muxer) SetWriter(w io.Writer) {
	self.w = w
	return
}

func (self *Muxer) WritePATPMT() (err error) {
	pat := tsio.PAT{
		Entries: []tsio.PATEntry{
			{ProgramNumber: 1, ProgramMapPID: tsio.PMT_PID},
		},
	}
	patlen := pat.Marshal(self.psidata[tsio.PSIHeaderLength:])
	n := tsio.FillPSI(self.psidata, tsio.TableIdPAT, tsio.TableExtPAT, patlen)
	self.datav[0] = self.psidata[:n]
	if err = self.tswpat.WritePackets(self.w, self.datav[:1], 0, false, true); err != nil {
		return
	}

	var elemStreams []tsio.ElementaryStreamInfo
	for _, stream := range self.streams {
		switch stream.Type() {
		case av.AAC:
			elemStreams = append(elemStreams, tsio.ElementaryStreamInfo{
				StreamType:    tsio.ElementaryStreamTypeAdtsAAC,
				ElementaryPID: stream.pid,
			})
		case av.H264:
			elemStreams = append(elemStreams, tsio.ElementaryStreamInfo{
				StreamType:    tsio.ElementaryStreamTypeH264,
				ElementaryPID: stream.pid,
			})
		}
	}

	pmt := tsio.PMT{
		PCRPID:                0x100,
		ElementaryStreamInfos: elemStreams,
	}
	pmtlen := pmt.Len()
	if pmtlen+tsio.PSIHeaderLength > len(self.psidata) {
		err = fmt.Errorf("ts: pmt too large")
		return
	}
	pmt.Marshal(self.psidata[tsio.PSIHeaderLength:])
	n = tsio.FillPSI(self.psidata, tsio.TableIdPMT, tsio.TableExtPMT, pmtlen)
	self.datav[0] = self.psidata[:n]
	if err = self.tswpmt.WritePackets(self.w, self.datav[:1], 0, false, true); err != nil {
		return
	}

	return
}

func (self *Muxer) WriteHeader(streams []av.CodecData) (err error) {
	if len(self.streams) == 0 {
		for _, stream := range streams {
			if err = self.newStream(stream); err != nil {
				return
			}
		}
	} else {
		for i, stream := range streams {
			if i < len(self.streams) {
				self.streams[i].CodecData = stream
			} else {
				if err = self.newStream(stream); err != nil {
					return
				}
			}
		}
	}

	if err = self.WritePATPMT(); err != nil {
		return
	}
	return
}

func (self *Muxer) WritePacket(pkt av.Packet) (err error) {
	stream := self.streams[pkt.Idx]
	pkt.Time += time.Second

	switch stream.Type() {
	case av.AAC:
		codec := stream.CodecData.(aacparser.CodecData)

		n := tsio.FillPESHeader(self.peshdr, tsio.StreamIdAAC, len(self.adtshdr)+len(pkt.Data), pkt.Time, 0)
		self.datav[0] = self.peshdr[:n]
		aacparser.FillADTSHeader(self.adtshdr, codec.Config, 1024, len(pkt.Data))
		self.datav[1] = self.adtshdr
		self.datav[2] = pkt.Data

		if err = stream.tsw.WritePackets(self.w, self.datav[:3], pkt.Time, true, false); err != nil {
			return
		}

	case av.H264:
		codec := stream.CodecData.(h264parser.CodecData)

		nalus := self.nalus[:0]
		if pkt.IsKeyFrame {
			nalus = append(nalus, codec.SPS())
			nalus = append(nalus, codec.PPS())
		}
		pktnalus, _ := h264parser.SplitNALUs(pkt.Data)
		for _, nalu := range pktnalus {
			nalus = append(nalus, nalu)
		}

		datav := self.datav[:1]
		for i, nalu := range nalus {
			if i == 0 {
				datav = append(datav, h264parser.AUDBytes)
			} else {
				datav = append(datav, h264parser.StartCodeBytes)
			}
			datav = append(datav, nalu)
		}

		n := tsio.FillPESHeader(self.peshdr, tsio.StreamIdH264, -1, pkt.Time+pkt.CompositionTime, pkt.Time)
		datav[0] = self.peshdr[:n]

		if err = stream.tsw.WritePackets(self.w, datav, pkt.Time, pkt.IsKeyFrame, false); err != nil {
			return
		}
	}

	return
}

type Demuxer struct {
	r *bufio.Reader

	pkts []av.Packet

	pat     *tsio.PAT
	pmt     *tsio.PMT
	streams []*Stream
	tshdr   []byte

	stage int
}

func NewDemuxer(r io.Reader) *Demuxer {
	return &Demuxer{
		tshdr: make([]byte, 188),
		r:     bufio.NewReaderSize(r, pio.RecommendBufioSize),
	}
}

func (self *Demuxer) Headers() (streams []av.CodecData, err error) {
	if err = self.probe(); err != nil {
		return
	}
	for _, stream := range self.streams {
		streams = append(streams, stream.CodecData)
	}
	return
}

func (self *Demuxer) probe() (err error) {
	if self.stage == 0 {
		for {
			if self.pmt != nil {
				n := 0
				for _, stream := range self.streams {
					if stream.CodecData != nil {
						n++
					}
				}
				if n == len(self.streams) {
					break
				}
			}
			if err = self.poll(); err != nil {
				return
			}
		}
		self.stage++
	}
	return
}

func (self *Demuxer) ReadPacket() (pkt av.Packet, err error) {
	if err = self.probe(); err != nil {
		return
	}

	for len(self.pkts) == 0 {
		if err = self.poll(); err != nil {
			return
		}
	}

	pkt = self.pkts[0]
	self.pkts = self.pkts[1:]
	return
}

func (self *Demuxer) poll() (err error) {
	if err = self.readTSPacket(); err == io.EOF {
		var n int
		if n, err = self.payloadEnd(); err != nil {
			return
		}
		if n == 0 {
			err = io.EOF
		}
	}
	return
}

func (self *Demuxer) initPMT(payload []byte) (err error) {
	var psihdrlen int
	var datalen int
	if _, _, psihdrlen, datalen, err = tsio.ParsePSI(payload); err != nil {
		return
	}
	self.pmt = &tsio.PMT{}
	if _, err = self.pmt.Unmarshal(payload[psihdrlen : psihdrlen+datalen]); err != nil {
		return
	}

	self.streams = []*Stream{}
	for i, info := range self.pmt.ElementaryStreamInfos {
		stream := &Stream{}
		stream.idx = i
		stream.demuxer = self
		stream.pid = info.ElementaryPID
		stream.streamType = info.StreamType
		switch info.StreamType {
		case tsio.ElementaryStreamTypeH264:
			self.streams = append(self.streams, stream)
		case tsio.ElementaryStreamTypeAdtsAAC:
			self.streams = append(self.streams, stream)
		}
	}
	return
}

func (self *Demuxer) payloadEnd() (n int, err error) {
	for _, stream := range self.streams {
		var i int
		if i, err = stream.payloadEnd(); err != nil {
			return
		}
		n += i
	}
	return
}

func (self *Demuxer) readTSPacket() (err error) {
	var hdrlen int
	var pid uint16
	var start bool
	var iskeyframe bool

	if _, err = io.ReadFull(self.r, self.tshdr); err != nil {
		return
	}

	if pid, start, iskeyframe, hdrlen, err = tsio.ParseTSHeader(self.tshdr); err != nil {
		return
	}
	payload := self.tshdr[hdrlen:]

	if self.pat == nil {
		if pid == 0 {
			var psihdrlen int
			var datalen int
			if _, _, psihdrlen, datalen, err = tsio.ParsePSI(payload); err != nil {
				return
			}
			self.pat = &tsio.PAT{}
			if _, err = self.pat.Unmarshal(payload[psihdrlen : psihdrlen+datalen]); err != nil {
				return
			}
		}
	} else if self.pmt == nil {
		for _, entry := range self.pat.Entries {
			if entry.ProgramMapPID == pid {
				if err = self.initPMT(payload); err != nil {
					return
				}
				break
			}
		}
	} else {
		for _, stream := range self.streams {
			if pid == stream.pid {
				if err = stream.handleTSPacket(start, iskeyframe, payload); err != nil {
					return
				}
				break
			}
		}
	}

	return
}

func (self *Stream) addPacket(payload []byte, timedelta time.Duration, dataType int8, headerChanged bool) {
	dts := self.dts
	pts := self.pts
	if dts == 0 {
		dts = pts
	}

	avcPacketType := flvio.AVC_NALU
	if dataType == flvio.TAG_AUDIO {
		avcPacketType = flvio.AAC_RAW
	}

	demuxer := self.demuxer
	pkt := av.Packet{
		Idx:           int8(self.idx),
		IsKeyFrame:    self.iskeyframe,
		Time:          dts + timedelta,
		Data:          payload,
		DataType:      dataType,
		HeaderChanged: headerChanged,
		AVCPacketType: uint8(avcPacketType),
	}
	if pts != dts {
		pkt.CompositionTime = pts - dts
	}
	demuxer.pkts = append(demuxer.pkts, pkt)
}

func (self *Stream) payloadEnd() (n int, err error) {
	payload := self.data
	if payload == nil {
		return
	}
	if self.datalen != 0 && len(payload) != self.datalen {
		err = fmt.Errorf("ts: packet size mismatch size=%d correct=%d", len(payload), self.datalen)
		return
	}
	self.data = nil

	switch self.streamType {
	case tsio.ElementaryStreamTypeAdtsAAC:
		var config aacparser.MPEG4AudioConfig
		headerChanged := false
		delta := time.Duration(0)
		for len(payload) > 0 {
			var hdrlen, framelen, samples int
			if config, hdrlen, framelen, samples, err = aacparser.ParseADTSHeader(payload); err != nil {
				return
			}
			if self.CodecData == nil {
				self.config = config
				err = self.updateAacCodec()
				if err != nil {
					return
				}
			} else if config != self.config {
				headerChanged = true
				self.config = config
				err = self.updateAacCodec()
				if err != nil {
					return
				}
			}
			self.addPacket(payload[hdrlen:framelen], delta, flvio.TAG_AUDIO, headerChanged)
			headerChanged = false
			n++
			delta += time.Duration(samples) * time.Second / time.Duration(config.SampleRate)
			payload = payload[framelen:]
		}

	case tsio.ElementaryStreamTypeH264:
		nalus, _ := h264parser.SplitNALUs(payload)
		var sps, pps []byte
		spsChange, ppsChange := 0, 0
		headerChanged := false
		for _, nalu := range nalus {
			if len(nalu) > 0 {
				naltype := nalu[0] & 0x1f
				switch {
				case naltype == h264parser.NALU_SPS:
					sps = nalu
					if self.sps != nil && !bytes.Equal(sps, self.sps) {
						spsChange = 1
						self.sps = sps
					}
				case naltype == h264parser.NALU_PPS:
					pps = nalu
					if self.pps != nil && !bytes.Equal(pps, self.pps) {
						ppsChange = 1
						self.pps = pps
					}
				case h264parser.IsDataNALU(nalu):
					// raw nalu to avcc
					b := make([]byte, 4+len(nalu))
					pio.PutU32BE(b[0:4], uint32(len(nalu)))
					copy(b[4:], nalu)
					//queueCursor will add headerChanged at first pkt
					if ppsChange != 0 && spsChange != 0 {
						headerChanged = true
						err = self.updateAvcCodec()
						if err != nil {
							return
						}
						ppsChange = 0
						spsChange = 0
					} else if ppsChange != 0 || spsChange != 0 {
						log.Error().Msg("SPS and PPS didnt change both")
					}
					self.addPacket(b, time.Duration(0), flvio.TAG_VIDEO, headerChanged)
					headerChanged = false
					n++
				}
			}
		}

		if self.CodecData == nil && len(sps) > 0 && len(pps) > 0 {
			self.sps = sps
			self.pps = pps
			err = self.updateAvcCodec()
			if err != nil {
				return
			}
		}

	}

	return
}

func (self *Stream) handleTSPacket(start bool, iskeyframe bool, payload []byte) (err error) {
	if start {
		if _, err = self.payloadEnd(); err != nil {
			return
		}
		var hdrlen int
		if hdrlen, _, self.datalen, self.pts, self.dts, err = tsio.ParsePESHeader(payload); err != nil {
			return
		}
		self.iskeyframe = iskeyframe
		if self.datalen == 0 {
			self.data = make([]byte, 0, 4096)
		} else {
			self.data = make([]byte, 0, self.datalen)
		}
		self.data = append(self.data, payload[hdrlen:]...)
	} else {
		self.data = append(self.data, payload...)
	}
	return
}

func (self *Stream) updateAacCodec() (err error) {

	codec, err := aacparser.NewCodecDataFromMPEG4AudioConfig(self.config)
	if err != nil {
		return
	}
	// hack gen flv aac_spec_config tag
	tag := flvio.Tag{
		Type:          flvio.TAG_AUDIO,
		SoundFormat:   flvio.SOUND_AAC,
		SoundRate:     flvio.SOUND_44Khz,
		AACPacketType: flvio.AAC_SEQHDR,
		Data:          codec.MPEG4AudioConfigBytes(),
	}
	switch codec.SampleFormat().BytesPerSample() {
	case 1:
		tag.SoundSize = flvio.SOUND_8BIT
	default:
		tag.SoundSize = flvio.SOUND_16BIT
	}
	switch codec.ChannelLayout().Count() {
	case 1:
		tag.SoundType = flvio.SOUND_MONO
	case 2:
		tag.SoundType = flvio.SOUND_STEREO
	}
	codec.SequnceHeaderTag = tag
	self.CodecData = codec
	return nil
}

func (self *Stream) updateAvcCodec() (err error) {

	codec, err := h264parser.NewCodecDataFromSPSAndPPS(self.sps, self.pps)
	if err != nil {
		return
	}
	// hack gen flv avc_spec_config tag
	tag := flvio.Tag{
		Type:          flvio.TAG_VIDEO,
		AVCPacketType: flvio.AVC_SEQHDR,
		CodecID:       flvio.VIDEO_H264,
		Data:          codec.AVCDecoderConfRecordBytes(),
		FrameType:     flvio.FRAME_KEY,
	}
	codec.SequnceHeaderTag = tag
	self.CodecData = codec
	return nil
}
