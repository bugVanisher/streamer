package slice

import (
	"bytes"
	"fmt"
	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"github.com/bugVanisher/streamer/utils"
	"time"
)

//切片类型: (00 flv header;01 script data;02 audio;03 video)
const (
	SLICE_TYPE_FLV_HEADER  = 0
	SLICE_TYPE_SCRIPT_DATA = 1
	SLICE_TYPE_AUDIO       = 2
	SLICE_TYPE_VIDEO       = 3
)

//帧类型: 00 非视频帧，音频或头部,01 IDR;02 有参考性帧;03无参考性帧;
const (
	SLICE_FRAME_TYPE_AUDIO = 0
	SLICE_FRAME_TYPE_IDR   = 1
	SLICE_FRAME_TYPE_REF   = 2
	SLICE_FRAME_TYPE_NOREF = 3
)

//当前切片在当前数据段的位 置，一共两个bits，最后一 个 bit=1 代表起始，第一个 bit=1 代表结束
//（eg: 00 中间； 01 起始非结束；10: 结束非起始；11 起始且结束）
const (
	SLICE_POSFLAG_MIDDLE   = 0
	SLICE_POSFLAG_START    = 1
	SLICE_POSFLAG_END      = 2
	SLICE_POSFLAG_STARTEND = 3
)

// flv header 切片ID
const (
	SLICE_ID_AVC_HEADER = 1
	SLICE_ID_AAC_HEADER = 2
)

const KSliceDefaultSizeMax = 1280
const KSliceHeaderSize = 15
const KSliceSizeThreshold = 3 << 10

// Packet 切片数据包结构
//｜SliceLen｜SliceType｜SliceSeq｜FrameId｜posFlag｜frameType｜reserved｜extended｜data  ｜
//｜UB12    ｜UB4      ｜Uint64  ｜Uint32 ｜UB2    ｜UB2      ｜UB3     ｜UB1     ｜string｜
type Packet struct {
	Size       uint16 // slice size
	SliceType  uint8  // slice type
	SliceId    uint64 // slice id
	FrameId    uint32 // frame id
	PosFlag    uint8
	FrameType  uint8
	Reserved   uint8
	ExtendFlag uint8
	Extend     Extend
	Data       []byte // slice data

	// no encode， for p2p quickOffset
	FrameDts      int32 // frame dts
	HeaderBeginAt int   // header pos in queue
	HeaderChanged bool  // indicates if the sps/pps info changed
}

func (p *Packet) IsHeader() bool {
	if p.SliceId == SLICE_ID_AVC_HEADER || p.SliceId == SLICE_ID_AAC_HEADER {
		return true
	}
	return false
}

type DataSliceInfo struct {
	SliceId      uint64 // slice id
	FrameId      uint32 // frame id
	SliceSizeMax int
}

func NewDataSliceInfo() *DataSliceInfo {
	s := &DataSliceInfo{}
	s.SliceId = uint64(time.Now().Unix() * 100000)
	s.FrameId = 0
	s.SliceSizeMax = KSliceDefaultSizeMax
	return s
}

func (s DataSliceInfo) SetSliceSizeMax(sliceSizeMax int) {
	s.SliceSizeMax = sliceSizeMax
}

func (s *DataSliceInfo) GenerateSlice(data []byte, avPkt *av.Packet) []Packet {
	dataSize := len(data)
	sliceCnt := (dataSize + s.SliceSizeMax - 1) / s.SliceSizeMax
	sliceDataSize := dataSize / sliceCnt
	overSize := dataSize - sliceDataSize*sliceCnt
	pkts := make([]Packet, 0, sliceCnt)
	var dataStartIndex int
	for i := 0; i < sliceCnt; i++ {
		var slicePkt Packet
		slicePkt.SliceId = s.SliceId
		slicePkt.FrameId = s.FrameId
		slicePkt.FrameDts = utils.TimeToTs(avPkt.Time)
		sliceDataSize = dataSize / sliceCnt
		if overSize > 0 {
			sliceDataSize++
			overSize--
		}
		// set SliceType && FrameType
		if avPkt.IsVideo() {
			slicePkt.SliceType = SLICE_TYPE_VIDEO
			slicePkt.FrameType = SLICE_FRAME_TYPE_REF
			if avPkt.IsKeyFrame {
				slicePkt.FrameType = SLICE_FRAME_TYPE_IDR
			}
		}
		if avPkt.DataType == av.FLV_TAG_AUDIO {
			slicePkt.SliceType = SLICE_TYPE_AUDIO
			slicePkt.FrameType = SLICE_FRAME_TYPE_AUDIO
		}
		// set PosFlag
		if sliceCnt == 1 {
			slicePkt.PosFlag = SLICE_POSFLAG_STARTEND
		} else {
			if i == 0 {
				slicePkt.PosFlag = SLICE_POSFLAG_START
			} else if i == sliceCnt-1 {
				slicePkt.PosFlag = SLICE_POSFLAG_END
			} else {
				slicePkt.PosFlag = SLICE_POSFLAG_MIDDLE
			}
		}
		// set Extend & Data
		slicePkt.ExtendFlag = KSliceExtendFlag
		if slicePkt.ExtendFlag > 0 {
			slicePkt.Extend = NewExtend()
			slicePkt.Extend[KSliceExtendKeyTimeStamp] = uint32(slicePkt.FrameDts)
			extendData := slicePkt.Extend.Encode()
			extendSize := uint16(len(extendData))
			slicePkt.Size = KSliceHeaderSize + extendSize + uint16(sliceDataSize)
			sliceHeader := makeSliceHeader(&slicePkt)
			dataEndIndex := dataStartIndex + sliceDataSize
			newData := make([]byte, int(slicePkt.Size))
			copy(newData, sliceHeader)
			copy(newData[KSliceHeaderSize:], extendData)
			copy(newData[KSliceHeaderSize+extendSize:], data[dataStartIndex:dataEndIndex])
			slicePkt.Data = newData
		} else {
			slicePkt.Size = KSliceHeaderSize + uint16(sliceDataSize)
			sliceHeader := makeSliceHeader(&slicePkt)
			dataEndIndex := dataStartIndex + sliceDataSize
			newData := make([]byte, int(slicePkt.Size))
			copy(newData, sliceHeader)
			copy(newData[KSliceHeaderSize:], data[dataStartIndex:dataEndIndex])
			slicePkt.Data = newData
		}

		pkts = append(pkts, slicePkt)
		dataStartIndex += sliceDataSize
		s.SliceId++
	}
	s.FrameId++
	return pkts
}

func GenerateHeaderSlice(data []byte, tag flvio.Tag) Packet {
	dataSize := len(data)
	sliceDataSize := dataSize
	var slicePkt Packet
	slicePkt.FrameId = 0
	slicePkt.FrameType = 0
	slicePkt.SliceType = SLICE_TYPE_FLV_HEADER
	slicePkt.PosFlag = SLICE_POSFLAG_STARTEND
	slicePkt.Size = uint16(sliceDataSize) + KSliceHeaderSize
	// set SliceType && FrameType
	if tag.Type == flvio.TAG_VIDEO {
		slicePkt.SliceId = SLICE_ID_AVC_HEADER
	}
	if tag.Type == flvio.TAG_AUDIO {
		slicePkt.SliceId = SLICE_ID_AAC_HEADER
	}

	slicePkt.Size = uint16(dataSize) + KSliceHeaderSize
	sliceHeader := makeSliceHeader(&slicePkt)
	newData := make([]byte, slicePkt.Size)
	copy(newData, sliceHeader)
	copy(newData[len(sliceHeader):], data)
	slicePkt.Data = newData

	return slicePkt
}

func makeSliceHeader(pkt *Packet) []byte {
	b := bytes.Buffer{}
	// pre generate header space size
	b.Grow(24)
	// slice size(12bit) & slice type(4bit)
	sizeType := (pkt.Size << 4) + uint16(pkt.SliceType)
	b.Write(utils.Uint16ToBytes(sizeType))
	// slice id
	b.Write(utils.Uint64ToBytes(pkt.SliceId))
	// frame id
	b.Write(utils.Uint32ToBytes(pkt.FrameId))
	// posFlag(2bit) & frameType(2bit) & Reserved(3bit) &extended(1bit)
	lastByte := (pkt.PosFlag << 6) + (pkt.FrameType << 4) + pkt.ExtendFlag
	b.WriteByte(lastByte)
	// extend

	return b.Bytes()
}

func ParseSliceHeader(data []byte) (pkt Packet, len uint16, err error) {
	sizeType := utils.BytesToUint16(data[0:2])
	pkt.Size = sizeType >> 4
	pkt.SliceType = uint8(sizeType & 0x0F)
	pkt.SliceId = utils.BytesToUint64(data[2:10])
	pkt.FrameId = utils.BytesToUint32(data[10:14])
	lastByte := data[14]
	pkt.PosFlag = lastByte >> 6
	pkt.FrameType = (lastByte >> 4) & 0x3
	pkt.Reserved = 0
	pkt.ExtendFlag = lastByte & 0x1

	len = pkt.Size
	if pkt.Size > KSliceSizeThreshold {
		err = fmt.Errorf("slice pkt size %d too big", pkt.Size)
	}
	return
}

const KSliceExtendFlag = 0
const KSliceExtendHeaderLen = 2

// extend key define
const (
	KSliceExtendKeyTimeStamp = 1
)

type Extend map[uint8]uint32

func NewExtend() Extend {
	var e Extend
	e = make(map[uint8]uint32)
	return e
}

func (e Extend) Encode() []byte {
	var w bytes.Buffer
	// extend length
	num := uint16(len(e)*5 + KSliceExtendHeaderLen)
	w.Write(utils.Uint16ToBytes(num))
	// key & value
	for key, value := range e {
		w.WriteByte(key)
		w.Write(utils.Uint32ToBytes(value))
	}

	return w.Bytes()
}

func (e Extend) Decode(data []byte) error {
	extendLen := utils.BytesToUint16(data[0:KSliceExtendHeaderLen])
	if ((extendLen - KSliceExtendHeaderLen) % 5) != 0 {
		return fmt.Errorf("extend len %d error", extendLen)
	}

	for i := KSliceExtendHeaderLen; i < int(extendLen); i = i + 5 {
		key := data[i]
		value := utils.BytesToUint32(data[i+1 : i+5])
		e[key] = value
	}
	return nil
}

type PacketWriter interface {
	WritePacket(Packet) error
}

type PacketReader interface {
	ReadPacket() (Packet, error)
}

type Muxer interface {
	WriteHeader(pkt []Packet) error // write the file header
	PacketWriter                    // write slice packets
	WriteTrailer() error            // finish writing file, this func can be called only once
}

// MuxCloser Muxer with Close() method
type MuxCloser interface {
	Muxer
	Close() error
}

type Demuxer interface {
	PacketReader                // read slice packets
	Headers() ([]Packet, error) // reads the file header, contains flv header infomations
}

// DemuxCloser Demuxer with Close() method
type DemuxCloser interface {
	Demuxer
	Close() error
}
