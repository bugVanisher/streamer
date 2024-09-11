package h264parser

import (
	"bytes"
	"encoding/binary"
	"fmt"
	jsoniter "github.com/json-iterator/go"
	"math"
	"time"

	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/utils/bits"
	"github.com/bugVanisher/streamer/utils/bits/pio"
)

const (
	NALU_SEI = 6
	NALU_SPS = 7
	NALU_PPS = 8
	NALU_AUD = 9
)

func IsDataNALU(b []byte) bool {
	typ := b[0] & 0x1f
	return typ >= 1 && typ <= 5
}

func IsSpsNALU(b byte) bool {
	typ := b & 0x1f
	return typ == NALU_SPS
}

func IsPpsNALU(b byte) bool {
	typ := b & 0x1f
	return typ == NALU_PPS
}

func IsSeiNALU(b byte) bool {
	typ := b & 0x1f
	return typ == NALU_SEI
}

/*
From: http://stackoverflow.com/questions/24884827/possible-locations-for-sequence-picture-parameter-sets-for-h-264-stream

First off, it's important to understand that there is no single standard H.264 elementary bitstream format. The specification document does contain an Annex, specifically Annex B, that describes one possible format, but it is not an actual requirement. The standard specifies how video is encoded into individual packets. How these packets are stored and transmitted is left open to the integrator.

1. Annex B
Network Abstraction Layer Units
The packets are called Network Abstraction Layer Units. Often abbreviated NALU (or sometimes just NAL) each packet can be individually parsed and processed. The first byte of each NALU contains the NALU type, specifically bits 3 through 7. (bit 0 is always off, and bits 1-2 indicate whether a NALU is referenced by another NALU).

There are 19 different NALU types defined separated into two categories, VCL and non-VCL:

VCL, or Video Coding Layer packets contain the actual visual information.
Non-VCLs contain metadata that may or may not be required to decode the video.
A single NALU, or even a VCL NALU is NOT the same thing as a frame. A frame can be ‘sliced’ into several NALUs. Just like you can slice a pizza. One or more slices are then virtually grouped into a Access Units (AU) that contain one frame. Slicing does come at a slight quality cost, so it is not often used.

Below is a table of all defined NALUs.

0      Unspecified                                                    non-VCL
1      Coded slice of a non-IDR picture                               VCL
2      Coded slice data partition A                                   VCL
3      Coded slice data partition B                                   VCL
4      Coded slice data partition C                                   VCL
5      Coded slice of an IDR picture                                  VCL
6      Supplemental enhancement information (SEI)                     non-VCL
7      Sequence parameter set                                         non-VCL
8      Picture parameter set                                          non-VCL
9      Access unit delimiter                                          non-VCL
10     End of sequence                                                non-VCL
11     End of stream                                                  non-VCL
12     Filler data                                                    non-VCL
13     Sequence parameter set extension                               non-VCL
14     Prefix NAL unit                                                non-VCL
15     Subset sequence parameter set                                  non-VCL
16     Depth parameter set                                            non-VCL
17..18 Reserved                                                       non-VCL
19     Coded slice of an auxiliary coded picture without partitioning non-VCL
20     Coded slice extension                                          non-VCL
21     Coded slice extension for depth view components                non-VCL
22..23 Reserved                                                       non-VCL
24..31 Unspecified                                                    non-VCL
There are a couple of NALU types where having knowledge of may be helpful later.

Sequence Parameter Set (SPS). This non-VCL NALU contains information required to configure the decoder such as profile, level, resolution, frame rate.
Picture Parameter Set (PPS). Similar to the SPS, this non-VCL contains information on entropy coding mode, slice groups, motion prediction and deblocking filters.
Instantaneous Decoder Refresh (IDR). This VCL NALU is a self contained image slice. That is, an IDR can be decoded and displayed without referencing any other NALU save SPS and PPS.
Access Unit Delimiter (AUD). An AUD is an optional NALU that can be use to delimit frames in an elementary stream. It is not required (unless otherwise stated by the container/protocol, like TS), and is often not included in order to save space, but it can be useful to finds the start of a frame without having to fully parse each NALU.
NALU Start Codes
A NALU does not contain is its size. Therefore simply concatenating the NALUs to create a stream will not work because you will not know where one stops and the next begins.

The Annex B specification solves this by requiring ‘Start Codes’ to precede each NALU. A start code is 2 or 3 0x00 bytes followed with a 0x01 byte. e.g. 0x000001 or 0x00000001.

The 4 byte variation is useful for transmission over a serial connection as it is trivial to byte align the stream by looking for 31 zero bits followed by a one. If the next bit is 0 (because every NALU starts with a 0 bit), it is the start of a NALU. The 4 byte variation is usually only used for signaling random access points in the stream such as a SPS PPS AUD and IDR Where as the 3 byte variation is used everywhere else to save space.

Emulation Prevention Bytes
Start codes work because the four byte sequences 0x000000, 0x000001, 0x000002 and 0x000003 are illegal within a non-RBSP NALU. So when creating a NALU, care is taken to escape these values that could otherwise be confused with a start code. This is accomplished by inserting an ‘Emulation Prevention’ byte 0x03, so that 0x000001 becomes 0x00000301.

When decoding, it is important to look for and ignore emulation prevention bytes. Because emulation prevention bytes can occur almost anywhere within a NALU, it is often more convenient in documentation to assume they have already been removed. A representation without emulation prevention bytes is called Raw Byte Sequence Payload (RBSP).

Example
Let's look at a complete example.

0x0000 | 00 00 00 01 67 64 00 0A AC 72 84 44 26 84 00 00
0x0010 | 03 00 04 00 00 03 00 CA 3C 48 96 11 80 00 00 00
0x0020 | 01 68 E8 43 8F 13 21 30 00 00 01 65 88 81 00 05
0x0030 | 4E 7F 87 DF 61 A5 8B 95 EE A4 E9 38 B7 6A 30 6A
0x0040 | 71 B9 55 60 0B 76 2E B5 0E E4 80 59 27 B8 67 A9
0x0050 | 63 37 5E 82 20 55 FB E4 6A E9 37 35 72 E2 22 91
0x0060 | 9E 4D FF 60 86 CE 7E 42 B7 95 CE 2A E1 26 BE 87
0x0070 | 73 84 26 BA 16 36 F4 E6 9F 17 DA D8 64 75 54 B1
0x0080 | F3 45 0C 0B 3C 74 B3 9D BC EB 53 73 87 C3 0E 62
0x0090 | 47 48 62 CA 59 EB 86 3F 3A FA 86 B5 BF A8 6D 06
0x00A0 | 16 50 82 C4 CE 62 9E 4E E6 4C C7 30 3E DE A1 0B
0x00B0 | D8 83 0B B6 B8 28 BC A9 EB 77 43 FC 7A 17 94 85
0x00C0 | 21 CA 37 6B 30 95 B5 46 77 30 60 B7 12 D6 8C C5
0x00D0 | 54 85 29 D8 69 A9 6F 12 4E 71 DF E3 E2 B1 6B 6B
0x00E0 | BF 9F FB 2E 57 30 A9 69 76 C4 46 A2 DF FA 91 D9
0x00F0 | 50 74 55 1D 49 04 5A 1C D6 86 68 7C B6 61 48 6C
0x0100 | 96 E6 12 4C 27 AD BA C7 51 99 8E D0 F0 ED 8E F6
0x0110 | 65 79 79 A6 12 A1 95 DB C8 AE E3 B6 35 E6 8D BC
0x0120 | 48 A3 7F AF 4A 28 8A 53 E2 7E 68 08 9F 67 77 98
0x0130 | 52 DB 50 84 D6 5E 25 E1 4A 99 58 34 C7 11 D6 43
0x0140 | FF C4 FD 9A 44 16 D1 B2 FB 02 DB A1 89 69 34 C2
0x0150 | 32 55 98 F9 9B B2 31 3F 49 59 0C 06 8C DB A5 B2
0x0160 | 9D 7E 12 2F D0 87 94 44 E4 0A 76 EF 99 2D 91 18
0x0170 | 39 50 3B 29 3B F5 2C 97 73 48 91 83 B0 A6 F3 4B
0x0180 | 70 2F 1C 8F 3B 78 23 C6 AA 86 46 43 1D D7 2A 23
0x0190 | 5E 2C D9 48 0A F5 F5 2C D1 FB 3F F0 4B 78 37 E9
0x01A0 | 45 DD 72 CF 80 35 C3 95 07 F3 D9 06 E5 4A 58 76
0x01B0 | 03 6C 81 20 62 45 65 44 73 BC FE C1 9F 31 E5 DB
0x01C0 | 89 5C 6B 79 D8 68 90 D7 26 A8 A1 88 86 81 DC 9A
0x01D0 | 4F 40 A5 23 C7 DE BE 6F 76 AB 79 16 51 21 67 83
0x01E0 | 2E F3 D6 27 1A 42 C2 94 D1 5D 6C DB 4A 7A E2 CB
0x01F0 | 0B B0 68 0B BE 19 59 00 50 FC C0 BD 9D F5 F5 F8
0x0200 | A8 17 19 D6 B3 E9 74 BA 50 E5 2C 45 7B F9 93 EA
0x0210 | 5A F9 A9 30 B1 6F 5B 36 24 1E 8D 55 57 F4 CC 67
0x0220 | B2 65 6A A9 36 26 D0 06 B8 E2 E3 73 8B D1 C0 1C
0x0230 | 52 15 CA B5 AC 60 3E 36 42 F1 2C BD 99 77 AB A8
0x0240 | A9 A4 8E 9C 8B 84 DE 73 F0 91 29 97 AE DB AF D6
0x0250 | F8 5E 9B 86 B3 B3 03 B3 AC 75 6F A6 11 69 2F 3D
0x0260 | 3A CE FA 53 86 60 95 6C BB C5 4E F3

This is a complete AU containing 3 NALUs. As you can see, we begin with a Start code followed by an SPS (SPS starts with 67). Within the SPS, you will see two Emulation Prevention bytes. Without these bytes the illegal sequence 0x000000 would occur at these positions. Next you will see a start code followed by a PPS (PPS starts with 68) and one final start code followed by an IDR slice. This is a complete H.264 stream. If you type these values into a hex editor and save the file with a .264 extension, you will be able to convert it to this image:

Lena

Annex B is commonly used in live and streaming formats such as transport streams, over the air broadcasts, and DVDs. In these formats it is common to repeat the SPS and PPS periodically, usually preceding every IDR thus creating a random access point for the decoder. This enables the ability to join a stream already in progress.

2. AVCC
The other common method of storing an H.264 stream is the AVCC format. In this format, each NALU is preceded with its length (in big endian format). This method is easier to parse, but you lose the byte alignment features of Annex B. Just to complicate things, the length may be encoded using 1, 2 or 4 bytes. This value is stored in a header object. This header is often called ‘extradata’ or ‘sequence header’. Its basic format is as follows:

bits
8   version ( always 0x01 )
8   avc profile ( sps[0][1] )
8   avc compatibility ( sps[0][2] )
8   avc level ( sps[0][3] )
6   reserved ( all bits on )
2   NALULengthSizeMinusOne
3   reserved ( all bits on )
5   number of SPS NALUs (usually 1)
repeated once per SPS:
  16         SPS size
	variable   SPS NALU data
8   number of PPS NALUs (usually 1)
repeated once per PPS
  16         PPS size
  variable   PPS NALU data

Using the same example above, the AVCC extradata will look like this:

0x0000 | 01 64 00 0A FF E1 00 19 67 64 00 0A AC 72 84 44
0x0010 | 26 84 00 00 03 00 04 00 00 03 00 CA 3C 48 96 11
0x0020 | 80 01 00 07 68 E8 43 8F 13 21 30

You will notice SPS and PPS is now stored out of band. That is, separate from the elementary stream data. Storage and transmission of this data is the job of the file container, and beyond the scope of this document. Notice that even though we are not using start codes, emulation prevention bytes are still inserted.

Additionally, there is a new variable called NALULengthSizeMinusOne. This confusingly named variable tells us how many bytes to use to store the length of each NALU. So, if NALULengthSizeMinusOne is set to 0, then each NALU is preceded with a single byte indicating its length. Using a single byte to store the size, the max size of a NALU is 255 bytes. That is obviously pretty small. Way too small for an entire key frame. Using 2 bytes gives us 64k per NALU. It would work in our example, but is still a pretty low limit. 3 bytes would be perfect, but for some reason is not universally supported. Therefore, 4 bytes is by far the most common, and it is what we used here:

0x0000 | 00 00 02 41 65 88 81 00 05 4E 7F 87 DF 61 A5 8B
0x0010 | 95 EE A4 E9 38 B7 6A 30 6A 71 B9 55 60 0B 76 2E
0x0020 | B5 0E E4 80 59 27 B8 67 A9 63 37 5E 82 20 55 FB
0x0030 | E4 6A E9 37 35 72 E2 22 91 9E 4D FF 60 86 CE 7E
0x0040 | 42 B7 95 CE 2A E1 26 BE 87 73 84 26 BA 16 36 F4
0x0050 | E6 9F 17 DA D8 64 75 54 B1 F3 45 0C 0B 3C 74 B3
0x0060 | 9D BC EB 53 73 87 C3 0E 62 47 48 62 CA 59 EB 86
0x0070 | 3F 3A FA 86 B5 BF A8 6D 06 16 50 82 C4 CE 62 9E
0x0080 | 4E E6 4C C7 30 3E DE A1 0B D8 83 0B B6 B8 28 BC
0x0090 | A9 EB 77 43 FC 7A 17 94 85 21 CA 37 6B 30 95 B5
0x00A0 | 46 77 30 60 B7 12 D6 8C C5 54 85 29 D8 69 A9 6F
0x00B0 | 12 4E 71 DF E3 E2 B1 6B 6B BF 9F FB 2E 57 30 A9
0x00C0 | 69 76 C4 46 A2 DF FA 91 D9 50 74 55 1D 49 04 5A
0x00D0 | 1C D6 86 68 7C B6 61 48 6C 96 E6 12 4C 27 AD BA
0x00E0 | C7 51 99 8E D0 F0 ED 8E F6 65 79 79 A6 12 A1 95
0x00F0 | DB C8 AE E3 B6 35 E6 8D BC 48 A3 7F AF 4A 28 8A
0x0100 | 53 E2 7E 68 08 9F 67 77 98 52 DB 50 84 D6 5E 25
0x0110 | E1 4A 99 58 34 C7 11 D6 43 FF C4 FD 9A 44 16 D1
0x0120 | B2 FB 02 DB A1 89 69 34 C2 32 55 98 F9 9B B2 31
0x0130 | 3F 49 59 0C 06 8C DB A5 B2 9D 7E 12 2F D0 87 94
0x0140 | 44 E4 0A 76 EF 99 2D 91 18 39 50 3B 29 3B F5 2C
0x0150 | 97 73 48 91 83 B0 A6 F3 4B 70 2F 1C 8F 3B 78 23
0x0160 | C6 AA 86 46 43 1D D7 2A 23 5E 2C D9 48 0A F5 F5
0x0170 | 2C D1 FB 3F F0 4B 78 37 E9 45 DD 72 CF 80 35 C3
0x0180 | 95 07 F3 D9 06 E5 4A 58 76 03 6C 81 20 62 45 65
0x0190 | 44 73 BC FE C1 9F 31 E5 DB 89 5C 6B 79 D8 68 90
0x01A0 | D7 26 A8 A1 88 86 81 DC 9A 4F 40 A5 23 C7 DE BE
0x01B0 | 6F 76 AB 79 16 51 21 67 83 2E F3 D6 27 1A 42 C2
0x01C0 | 94 D1 5D 6C DB 4A 7A E2 CB 0B B0 68 0B BE 19 59
0x01D0 | 00 50 FC C0 BD 9D F5 F5 F8 A8 17 19 D6 B3 E9 74
0x01E0 | BA 50 E5 2C 45 7B F9 93 EA 5A F9 A9 30 B1 6F 5B
0x01F0 | 36 24 1E 8D 55 57 F4 CC 67 B2 65 6A A9 36 26 D0
0x0200 | 06 B8 E2 E3 73 8B D1 C0 1C 52 15 CA B5 AC 60 3E
0x0210 | 36 42 F1 2C BD 99 77 AB A8 A9 A4 8E 9C 8B 84 DE
0x0220 | 73 F0 91 29 97 AE DB AF D6 F8 5E 9B 86 B3 B3 03
0x0230 | B3 AC 75 6F A6 11 69 2F 3D 3A CE FA 53 86 60 95
0x0240 | 6C BB C5 4E F3

An advantage to this format is the ability to configure the decoder at the start and jump into the middle of a stream. This is a common use case where the media is available on a random access medium such as a hard drive, and is therefore used in common container formats such as MP4 and MKV.
*/

var StartCodeBytes = []byte{0, 0, 1}
var AUDBytes = []byte{0, 0, 0, 1, 0x9, 0xf0, 0, 0, 0, 1} // AUD

func CheckNALUsType(b []byte) (typ int) {
	_, typ = SplitNALUs(b)
	return
}

const (
	NALU_RAW = iota
	NALU_AVCC
	NALU_ANNEXB
)

func SplitNALUs(b []byte) (nalus [][]byte, typ int) {
	if len(b) < 4 {
		return [][]byte{b}, NALU_RAW
	}

	val3 := pio.U24BE(b)
	val4 := pio.U32BE(b)

	// maybe AVCC
	if val4 <= uint32(len(b)) {
		_val4 := val4
		_b := b[4:]
		nalus := [][]byte{}
		for {
			if _val4 > uint32(len(_b)) {
				break
			}
			nalus = append(nalus, _b[:_val4])
			_b = _b[_val4:]
			if len(_b) < 4 {
				break
			}
			_val4 = pio.U32BE(_b)
			_b = _b[4:]
			if _val4 > uint32(len(_b)) {
				break
			}
		}
		if len(_b) == 0 {
			return nalus, NALU_AVCC
		}
	}

	// is Annex B
	if val3 == 1 || val4 == 1 {
		_val3 := val3
		_val4 := val4
		start := 0
		pos := 0
		for {
			if start != pos {
				nalus = append(nalus, b[start:pos])
			}
			if _val3 == 1 {
				pos += 3
			} else if _val4 == 1 {
				pos += 4
			}
			start = pos
			if start == len(b) {
				break
			}
			_val3 = 0
			_val4 = 0
			for pos < len(b) {
				if pos+2 < len(b) && b[pos] == 0 {
					_val3 = pio.U24BE(b[pos:])
					if _val3 == 0 {
						if pos+3 < len(b) {
							_val4 = uint32(b[pos+3])
							if _val4 == 1 {
								break
							}
						}
					} else if _val3 == 1 {
						break
					}
					pos++
				} else {
					pos++
				}
			}
		}
		typ = NALU_ANNEXB
		return
	}

	return [][]byte{b}, NALU_RAW
}

//VuiParameters ...
type VuiParameters struct {
	AspectRatioInfoPresentFlag     uint
	AspectRatioIdc                 uint
	SarWidth                       uint
	SarHeight                      uint
	OverscanInfoPresentFlag        uint
	OverscanAppropriateFlag        uint
	VideoSignalTypePresentFlag     uint
	VideoFormat                    uint
	VideoFullRangeRlag             uint
	ColourDescriptionPresentFlag   uint
	ColourPrimaries                uint
	TransferCharacteristics        uint
	MatrixCoefficients             uint
	ChromaLocInfoPresentFlag       uint
	ChromaSampleLocTypeTopField    uint
	ChromaSampleLocTypeBottomField uint
	TimingInfoPresentFlag          uint
	NumUnitsInTick                 uint
	TimeScale                      uint
	FixedFrameRateFlag             uint
	FPS                            uint
	/*NalHrdParametersPresentFlag         uint
	VclHrdParametersPresentFlag         uint
	LowDelayHrdFlag                      uint
	PicStructPresentFlag                 uint
	BitstreamRestrictionFlag              uint
	MotionVectorsOverPicBoundariesFlag uint
	MaxBytesPerPicDenom                 uint
	MaxBitsPerMbDenom                   uint
	Log2MaxMvLengthHorizontal           uint
	Log2MaxMvLengthVertical             uint
	MumReorderFrames                      uint
	MaxDecFrameBuffering                 uint*/
}

//SPSInfo ...
type SPSInfo struct {
	Id               uint
	ForbiddenZeroBit uint
	NalRefIdc        uint
	NalUnitType      uint

	ConstraintSetFlag []uint

	ProfileIdc        uint
	LevelIdc          uint
	SeqParameterSetID uint

	ChromaFormatIdc                 uint
	SeparateColourPlaneFlag         uint
	BitDepthLumaMinus8              uint
	BitDepthChromaMinus8            uint
	QpprimeYZeroTransformBypassFlag uint
	SeqScalingMatrixPresentFlag     uint
	SeqScalingListPresentFlag       []uint

	Log2MaxFrameNumMinus4          uint
	PicOrderCntType                uint
	Log2MaxPicOrderCntLsbMinus4    uint
	DeltaPicOrderAlwaysZeroFlag    uint
	OffsetForNonRefPic             uint
	OffsetForTopToBottomField      uint
	NumRefFramesInPicOrderCntCycle uint
	MaxNumRefFrames                uint
	GapsInFrameNumValueAllowedFlag uint

	PicWidthInMbsMinus1       uint
	PicHeightInMapUnitsMinus1 uint
	MbAdaptiveFrameFieldFlag  uint
	Direct8x8InferenceFlag    uint

	FrameMbsOnlyFlag  uint
	FrameCroppingFlag uint

	CropLeft   uint
	CropRight  uint
	CropTop    uint
	CropBottom uint

	MbWidth  uint
	MbHeight uint

	Width  uint
	Height uint

	VuiParametersPresentFlag uint

	VuiParameters

	FPS uint
}

//PPSInfo ...
type PPSInfo struct {
	ForbiddenZeroBit uint
	NalRefIdc        uint
	NalUnitType      uint

	PicParameterSetID                  uint
	SeqParameterSetID                  uint
	EntropyCodingModeFlag              uint
	PicOrderPresentFlag                uint
	NumSliceGroupsMinus1               uint
	SliceGroupMapType                  uint
	RunLengthMinus1                    []uint // up to num_slice_groups_minus1, which is <= 7 in Baseline and Extended, 0 otheriwse
	TopLeft                            []uint
	BottomRight                        []uint
	SliceGroupChangeDirectionFlag      uint
	SliceGroupChangeRateMinus1         uint
	PicSizeInMapUnitsMinus1            uint
	SliceGroupID                       []uint
	NumRefIdxL0ActiveMinus1            uint
	NumRefIdxL1ActiveMinus1            uint
	WeightedPredFlag                   uint
	WeightedBipredIdc                  uint
	PicInitQpMinus26                   uint
	PicInitQsMinus26                   uint
	ChromaQpIndexOffset                uint
	DeblockingFilterControlPresentFlag uint
	ConstrainedIntraPredFlag           uint
	RedundantPicCntPresnetFlag         uint

	//todo more rbsp data 待实现
}

//SEIInfo ...
type SEIInfo struct {
	ForbiddenZeroBit uint
	NalRefIdc        uint
	NalUnitType      uint
	PayloadType      uint
	PayloadSize      uint

	// PayloadType == 5
	UUID     []byte
	UserData []byte

	// PayloadType == 242
	Ts   uint64
	Data []byte
}

//去除防竞争码0x000003
func DeEmulationPrevention(data []byte) []byte {
	dataLen := len(data)
	dataCopy := make([]byte, dataLen) //在copy前申请足够空间,copy操作会进行深拷贝,否则还是浅拷贝
	copy(dataCopy, data)
	for i := 0; i < dataLen-2; i++ {
		//check 0x000003
		if dataCopy[i] == 0x00 && dataCopy[i+1] == 0x00 && dataCopy[i+2] == 0x03 {
			//去掉0x03
			dataCopy = append(dataCopy[:i+2], dataCopy[i+3:]...)
			dataLen--
		}
	}
	return dataCopy
}

func RemoveH264orH265EmulationBytes(b []byte) []byte {
	j := 0
	r := make([]byte, len(b))
	for i := 0; (i < len(b)) && (j < len(b)); {
		if i+2 < len(b) &&
			b[i] == 0 && b[i+1] == 0 && b[i+2] == 3 {
			r[j] = 0
			r[j+1] = 0
			j = j + 2
			i = i + 3
		} else {
			r[j] = b[i]
			j = j + 1
			i = i + 1
		}
	}
	return r[:j]
}

//增加防竞争码0x000003
// 0x00 00 01  -----> 0x00 00 03 01
func AddEmulationPrevention(data []byte) ([]byte, int) {
	dataLen := len(data)
	dataCopy := make([]byte, dataLen) //在copy前申请足够空间,copy操作会进行深拷贝,否则还是浅拷贝
	copy(dataCopy, data)
	addCnt := 0
	for i := 0; i < dataLen-2; i++ {
		//check 0x000000-0x000003
		if dataCopy[i] == 0x00 && dataCopy[i+1] == 0x00 && dataCopy[i+2] <= 3 {
			//add 0x03
			dataCopy = append(dataCopy[:i+3], dataCopy[i+2:]...)
			dataCopy[i+2] = 0x03
			addCnt++
		}
	}
	return dataCopy, addCnt
}

//ParseSPS ...
func ParseSPS(data []byte) (sps SPSInfo, err error) {
	bs := DeEmulationPrevention(data)
	r := &bits.GolombBitReader{R: bytes.NewReader(bs)}

	//forbidden_zero_bit
	if sps.ForbiddenZeroBit, err = r.ReadBit(); err != nil {
		return
	}
	//nal_ref_idc
	if sps.NalRefIdc, err = r.ReadBits(2); err != nil {
		return
	}
	//nal_unit_type
	if sps.NalUnitType, err = r.ReadBits(5); err != nil {
		return
	}

	if sps.ProfileIdc, err = r.ReadBits(8); err != nil {
		return
	}
	//constraint_set0_flag - constraint_set5_flag
	sps.ConstraintSetFlag = make([]uint, 6)
	for i := 0; i < 6; i++ {
		if sps.ConstraintSetFlag[i], err = r.ReadBit(); err != nil {
			return
		}
	}
	//reserved_zero_2bits
	if _, err = r.ReadBits(2); err != nil {
		return
	}

	// level_idc
	if sps.LevelIdc, err = r.ReadBits(8); err != nil {
		return
	}

	// seq_parameter_set_id
	if sps.SeqParameterSetID, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	if sps.ProfileIdc == 100 || sps.ProfileIdc == 110 ||
		sps.ProfileIdc == 122 || sps.ProfileIdc == 244 ||
		sps.ProfileIdc == 44 || sps.ProfileIdc == 83 ||
		sps.ProfileIdc == 86 || sps.ProfileIdc == 118 ||
		sps.ProfileIdc == 128 || sps.ProfileIdc == 138 ||
		sps.ProfileIdc == 139 || sps.ProfileIdc == 134 ||
		sps.ProfileIdc == 135 {

		if sps.ChromaFormatIdc, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}

		if sps.ChromaFormatIdc == 3 {
			//SeparateColourPlaneFlag
			if sps.SeparateColourPlaneFlag, err = r.ReadBit(); err != nil {
				return
			}
		}

		// bit_depth_luma_minus8
		if sps.BitDepthLumaMinus8, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		// bit_depth_chroma_minus8
		if sps.BitDepthChromaMinus8, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		// qpprime_y_zero_transform_bypass_flag
		if sps.QpprimeYZeroTransformBypassFlag, err = r.ReadBit(); err != nil {
			return
		}

		if sps.SeqScalingMatrixPresentFlag, err = r.ReadBit(); err != nil {
			return
		}

		if sps.SeqScalingMatrixPresentFlag != 0 {
			scalingListCount := 8
			if sps.ChromaFormatIdc == 3 {
				scalingListCount = 12
			}
			sps.SeqScalingListPresentFlag = make([]uint, scalingListCount)
			for i := 0; i < scalingListCount; i++ {
				if sps.SeqScalingListPresentFlag[i], err = r.ReadBit(); err != nil {
					return
				}
				if sps.SeqScalingListPresentFlag[i] != 0 {
					//todo 内容太多,暂不记录
					var sizeOfScalingList uint
					if i < 6 {
						sizeOfScalingList = 16
					} else {
						sizeOfScalingList = 64
					}
					lastScale := uint(8)
					nextScale := uint(8)
					for j := uint(0); j < sizeOfScalingList; j++ {
						if nextScale != 0 {
							var delta_scale uint
							if delta_scale, err = r.ReadSE(); err != nil {
								return
							}
							nextScale = (lastScale + delta_scale + 256) % 256
						}
						if nextScale != 0 {
							lastScale = nextScale
						}
					}
				}
			}
		}
	}

	// log2_max_frame_num_minus4
	if sps.Log2MaxFrameNumMinus4, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	if sps.PicOrderCntType, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if sps.PicOrderCntType == 0 {
		// log2_max_pic_order_cnt_lsb_minus4
		if sps.Log2MaxPicOrderCntLsbMinus4, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
	} else if sps.PicOrderCntType == 1 {
		// delta_pic_order_always_zero_flag
		if sps.DeltaPicOrderAlwaysZeroFlag, err = r.ReadBit(); err != nil {
			return
		}
		// offset_for_non_ref_pic
		if sps.OffsetForNonRefPic, err = r.ReadSE(); err != nil {
			return
		}
		// offset_for_top_to_bottom_field
		if sps.OffsetForTopToBottomField, err = r.ReadSE(); err != nil {
			return
		}
		// num_ref_frames_in_pic_order_cnt_cycle
		if sps.NumRefFramesInPicOrderCntCycle, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		for i := uint(0); i < sps.NumRefFramesInPicOrderCntCycle; i++ {
			//todo 内容太多，暂不记录
			if _, err = r.ReadSE(); err != nil {
				return
			}
		}
	}

	// max_num_ref_frames
	if sps.MaxNumRefFrames, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	// gaps_in_frame_num_value_allowed_flag
	if sps.GapsInFrameNumValueAllowedFlag, err = r.ReadBit(); err != nil {
		return
	}

	if sps.PicWidthInMbsMinus1, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	//sps.MbWidth++

	if sps.PicHeightInMapUnitsMinus1, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	//sps.MbHeight++

	//frame_mbs_only_flag
	if sps.FrameMbsOnlyFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.FrameMbsOnlyFlag == 0 {
		// mb_adaptive_frame_field_flag
		if sps.MbAdaptiveFrameFieldFlag, err = r.ReadBit(); err != nil {
			return
		}
	}

	// direct_8x8_inference_flag
	if sps.Direct8x8InferenceFlag, err = r.ReadBit(); err != nil {
		return
	}

	//frame_cropping_flag
	if sps.FrameCroppingFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.FrameCroppingFlag != 0 {
		if sps.CropLeft, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if sps.CropRight, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if sps.CropTop, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if sps.CropBottom, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
	}

	sps.Width = ((sps.PicWidthInMbsMinus1 + 1) * 16) - sps.CropLeft*2 - sps.CropRight*2
	sps.Height = ((2 - sps.FrameMbsOnlyFlag) * (sps.PicHeightInMapUnitsMinus1 + 1) * 16) - sps.CropTop*2 - sps.CropBottom*2

	if sps.VuiParametersPresentFlag, err = r.ReadBit(); err != nil {
		return
	}

	if sps.VuiParametersPresentFlag != 0 {
		err = parseVuiParameters(&sps, r)
	}

	return
}

func parseVuiParameters(sps *SPSInfo, r *bits.GolombBitReader) (err error) {

	if sps.AspectRatioInfoPresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.AspectRatioInfoPresentFlag != 0 {
		if sps.AspectRatioIdc, err = r.ReadBits(8); err != nil {
			return
		}
		if sps.AspectRatioIdc == 255 { //SAR_Extended
			if sps.SarWidth, err = r.ReadBits(16); err != nil {
				return
			}
			if sps.SarHeight, err = r.ReadBits(16); err != nil {
				return
			}
		}
	}

	if sps.OverscanInfoPresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.OverscanInfoPresentFlag != 0 {
		if sps.OverscanAppropriateFlag, err = r.ReadBit(); err != nil {
			return
		}
	}

	if sps.VideoSignalTypePresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.VideoSignalTypePresentFlag != 0 {
		if sps.VideoFormat, err = r.ReadBits(3); err != nil {
			return
		}
		if sps.VideoFullRangeRlag, err = r.ReadBit(); err != nil {
			return
		}
		if sps.ColourDescriptionPresentFlag, err = r.ReadBit(); err != nil {
			return
		}
		if sps.ColourDescriptionPresentFlag != 0 {
			if sps.ColourPrimaries, err = r.ReadBits(8); err != nil {
				return
			}
			if sps.TransferCharacteristics, err = r.ReadBits(8); err != nil {
				return
			}
			if sps.MatrixCoefficients, err = r.ReadBits(8); err != nil {
				return
			}
		}
	}

	if sps.ChromaLocInfoPresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.ChromaLocInfoPresentFlag != 0 {
		if sps.ChromaSampleLocTypeTopField, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if sps.ChromaSampleLocTypeBottomField, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
	}

	if sps.TimingInfoPresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if sps.TimingInfoPresentFlag != 0 {
		if sps.NumUnitsInTick, err = r.ReadBits(32); err != nil {
			return
		}
		if sps.TimeScale, err = r.ReadBits(32); err != nil {
			return
		}
		if sps.FixedFrameRateFlag, err = r.ReadBit(); err != nil {
			return
		}
		if sps.NumUnitsInTick > 0 {
			sps.FPS = sps.TimeScale / sps.NumUnitsInTick
			if sps.FixedFrameRateFlag != 0 {
				sps.FPS /= 2
			}
		}
	}

	//todo 后续部分参数待实现

	return
}

//ParsePPS ...
func ParsePPS(data []byte) (pps PPSInfo, err error) {
	bs := DeEmulationPrevention(data)
	r := &bits.GolombBitReader{R: bytes.NewReader(bs)}

	//forbidden_zero_bit
	if pps.ForbiddenZeroBit, err = r.ReadBit(); err != nil {
		return
	}
	//nal_ref_idc
	if pps.NalRefIdc, err = r.ReadBits(2); err != nil {
		return
	}
	//nal_unit_type
	if pps.NalUnitType, err = r.ReadBits(5); err != nil {
		return
	}

	if pps.PicParameterSetID, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if pps.SeqParameterSetID, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if pps.EntropyCodingModeFlag, err = r.ReadBit(); err != nil {
		return
	}
	if pps.PicOrderPresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if pps.NumSliceGroupsMinus1, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if pps.NumSliceGroupsMinus1 > 0 {
		if pps.SliceGroupMapType, err = r.ReadExponentialGolombCode(); err != nil {
			return
		}
		if pps.SliceGroupMapType == 0 {
			pps.RunLengthMinus1 = make([]uint, pps.NumSliceGroupsMinus1+1)
			for i := uint(0); i <= pps.NumSliceGroupsMinus1; i++ {
				if pps.RunLengthMinus1[i], err = r.ReadExponentialGolombCode(); err != nil {
					return
				}
			}
		} else if pps.SliceGroupMapType == 2 {
			pps.TopLeft = make([]uint, pps.NumSliceGroupsMinus1)
			pps.BottomRight = make([]uint, pps.NumSliceGroupsMinus1)
			for i := uint(0); i < pps.NumSliceGroupsMinus1; i++ {
				if pps.TopLeft[i], err = r.ReadExponentialGolombCode(); err != nil {
					return
				}
				if pps.BottomRight[i], err = r.ReadExponentialGolombCode(); err != nil {
					return
				}
			}
		} else if pps.SliceGroupMapType == 3 || pps.SliceGroupMapType == 4 || pps.SliceGroupMapType == 5 {
			if pps.SliceGroupChangeDirectionFlag, err = r.ReadBit(); err != nil {
				return
			}
			if pps.SliceGroupChangeRateMinus1, err = r.ReadExponentialGolombCode(); err != nil {
				return
			}
		} else if pps.SliceGroupMapType == 6 {
			if pps.PicSizeInMapUnitsMinus1, err = r.ReadExponentialGolombCode(); err != nil {
				return
			}
			pps.SliceGroupID = make([]uint, pps.PicSizeInMapUnitsMinus1+1)
			v := int(math.Log2(float64(pps.NumSliceGroupsMinus1 + 1)))
			for i := uint(0); i <= pps.PicSizeInMapUnitsMinus1; i++ {
				if pps.SliceGroupID[i], err = r.ReadBits(v); err != nil {
					return
				}
			}
		}
	}

	if pps.NumRefIdxL0ActiveMinus1, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if pps.NumRefIdxL1ActiveMinus1, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}
	if pps.WeightedPredFlag, err = r.ReadBit(); err != nil {
		return
	}
	if pps.WeightedBipredIdc, err = r.ReadBits(2); err != nil {
		return
	}
	if pps.PicInitQpMinus26, err = r.ReadSE(); err != nil {
		return
	}
	if pps.PicInitQsMinus26, err = r.ReadSE(); err != nil {
		return
	}
	if pps.ChromaQpIndexOffset, err = r.ReadSE(); err != nil {
		return
	}
	if pps.DeblockingFilterControlPresentFlag, err = r.ReadBit(); err != nil {
		return
	}
	if pps.ConstrainedIntraPredFlag, err = r.ReadBit(); err != nil {
		return
	}
	if pps.RedundantPicCntPresnetFlag, err = r.ReadBit(); err != nil {
		return
	}

	return
}

//ParseSEI ...
func ParseSEI(data []byte) (sei SEIInfo, err error) {

	r := &bits.GolombBitReader{R: bytes.NewReader(data)}

	//forbidden_zero_bit
	if sei.ForbiddenZeroBit, err = r.ReadBit(); err != nil {
		return
	}
	//nal_ref_idc
	if sei.NalRefIdc, err = r.ReadBits(2); err != nil {
		return
	}
	//nal_unit_type
	if sei.NalUnitType, err = r.ReadBits(5); err != nil {
		return
	}

	for {
		var payloadtype uint
		if payloadtype, err = r.ReadBits(8); err != nil {
			return
		}
		sei.PayloadType += payloadtype
		if payloadtype != 255 {
			break
		}
	}
	for {
		var payloadsize uint
		if payloadsize, err = r.ReadBits(8); err != nil {
			return
		}
		sei.PayloadSize += payloadsize
		if payloadsize != 255 {
			break
		}
	}

	if sei.PayloadType == 5 {
		sei.UUID = make([]byte, 16)
		for i := 0; i < 16; i++ {
			var b uint
			if b, err = r.ReadBits(8); err != nil {
				return
			}
			sei.UUID = append(sei.UUID, byte(b))
		}
		sei.UserData = make([]byte, sei.PayloadSize-16)
		for i := uint(0); i < sei.PayloadSize-16; i++ {
			var b uint
			if b, err = r.ReadBits(8); err != nil {
				return
			}
			sei.UserData = append(sei.UserData, byte(b))
		}
	} else if sei.PayloadType == 242 {
		if sei.PayloadSize == 8 {
			sei.Data = make([]byte, 8)
			for i := 0; i < 8; i++ {
				var b uint
				if b, err = r.ReadBits(8); err != nil {
					return
				}
				sei.Data = append(sei.Data, byte(b))
			}
			if len(sei.Data) >= 8 {
				sei.Ts = binary.BigEndian.Uint64(sei.Data)
			}
		} else {
			data := make([]byte, sei.PayloadSize)
			for i := uint(0); i < sei.PayloadSize; i++ {
				var b uint
				if b, err = r.ReadBits(8); err != nil {
					return
				}
				data = append(data, byte(b))
			}
			info := struct {
				Ts uint64 `json:"ts"`
			}{}
			sei.Data = data[bytes.LastIndexByte(data, 0)+1:]
			err := jsoniter.Unmarshal(sei.Data, &info)
			if err == nil {
				sei.Ts = info.Ts
			}
		}
	}

	return
}

type CodecData struct {
	Record     []byte
	RecordInfo AVCDecoderConfRecord
	SPSInfo    SPSInfo
	PPSInfo    PPSInfo

	SequnceHeaderTag interface{}
}

func (self CodecData) Type() av.CodecType {
	return av.H264
}

func (self CodecData) AVCDecoderConfRecordBytes() []byte {
	return self.Record
}

func (self CodecData) SPS() []byte {
	return self.RecordInfo.SPS[0]
}

func (self CodecData) PPS() []byte {
	return self.RecordInfo.PPS[0]
}

func (self CodecData) Width() int {
	return int(self.SPSInfo.Width)
}

func (self CodecData) Height() int {
	return int(self.SPSInfo.Height)
}

func (self CodecData) FPS() int {
	return int(self.SPSInfo.FPS)
}

func (self CodecData) Resolution() string {
	return fmt.Sprintf("%vx%v", self.Width(), self.Height())
}

func (self CodecData) Tag() string {
	return fmt.Sprintf("avc1.%02X%02X%02X", self.RecordInfo.AVCProfileIndication, self.RecordInfo.ProfileCompatibility, self.RecordInfo.AVCLevelIndication)
}

func (self CodecData) Bandwidth() string {
	return fmt.Sprintf("%v", (int(float64(self.Width())*(float64(1.71)*(30/float64(self.FPS())))))*1000)
}

func (self CodecData) PacketDuration(data []byte) time.Duration {
	return time.Duration(1000./float64(self.FPS())) * time.Millisecond
}

func NewCodecDataFromAVCDecoderConfRecord(record []byte) (self CodecData, err error) {
	self.Record = record
	if _, err = (&self.RecordInfo).Unmarshal(record); err != nil {
		return
	}
	if len(self.RecordInfo.SPS) == 0 {
		err = fmt.Errorf("h264parser: no SPS found in AVCDecoderConfRecord")
		return
	}
	if len(self.RecordInfo.PPS) == 0 {
		err = fmt.Errorf("h264parser: no PPS found in AVCDecoderConfRecord")
		return
	}
	if self.SPSInfo, err = ParseSPS(self.RecordInfo.SPS[0]); err != nil {
		err = fmt.Errorf("h264parser: parse SPS failed(%s)", err)
		return
	}
	if self.PPSInfo, err = ParsePPS(self.RecordInfo.PPS[0]); err != nil {
		err = fmt.Errorf("h264parser: parse PPS failed(%s)", err)
		return
	}
	return
}

func NewCodecDataFromSPSAndPPS(sps, pps []byte) (self CodecData, err error) {
	if len(sps) < 4 {
		err = ErrDecconfInvalid
		return
	}
	recordinfo := AVCDecoderConfRecord{}
	recordinfo.AVCProfileIndication = sps[1]
	recordinfo.ProfileCompatibility = sps[2]
	recordinfo.AVCLevelIndication = sps[3]
	recordinfo.SPS = [][]byte{sps}
	recordinfo.PPS = [][]byte{pps}
	recordinfo.LengthSizeMinusOne = 3

	buf := make([]byte, recordinfo.Len())
	recordinfo.Marshal(buf)

	self.RecordInfo = recordinfo
	self.Record = buf

	if len(self.RecordInfo.SPS) > 0 {
		if self.SPSInfo, err = ParseSPS(self.RecordInfo.SPS[0]); err != nil {
			return
		}
	}
	if len(self.RecordInfo.PPS) > 0 {
		if self.PPSInfo, err = ParsePPS(self.RecordInfo.PPS[0]); err != nil {
			return
		}
	}
	return
}

type AVCDecoderConfRecord struct {
	AVCProfileIndication uint8
	ProfileCompatibility uint8
	AVCLevelIndication   uint8
	LengthSizeMinusOne   uint8
	SPS                  [][]byte
	PPS                  [][]byte
}

var ErrDecconfInvalid = fmt.Errorf("h264parser: AVCDecoderConfRecord invalid")

func (self *AVCDecoderConfRecord) Unmarshal(b []byte) (n int, err error) {
	if len(b) < 7 {
		err = ErrDecconfInvalid
		return
	}

	self.AVCProfileIndication = b[1]
	self.ProfileCompatibility = b[2]
	self.AVCLevelIndication = b[3]
	self.LengthSizeMinusOne = b[4] & 0x03
	spscount := int(b[5] & 0x1f)
	n += 6

	for i := 0; i < spscount; i++ {
		if len(b) < n+2 {
			err = ErrDecconfInvalid
			return
		}
		spslen := int(pio.U16BE(b[n:]))
		n += 2

		if len(b) < n+spslen {
			err = ErrDecconfInvalid
			return
		}
		self.SPS = append(self.SPS, b[n:n+spslen])
		n += spslen
	}

	if len(b) < n+1 {
		err = ErrDecconfInvalid
		return
	}
	ppscount := int(b[n])
	n++

	for i := 0; i < ppscount; i++ {
		if len(b) < n+2 {
			err = ErrDecconfInvalid
			return
		}
		ppslen := int(pio.U16BE(b[n:]))
		n += 2

		if len(b) < n+ppslen {
			err = ErrDecconfInvalid
			return
		}
		self.PPS = append(self.PPS, b[n:n+ppslen])
		n += ppslen
	}

	return
}

func (self AVCDecoderConfRecord) Len() (n int) {
	n = 7
	for _, sps := range self.SPS {
		n += 2 + len(sps)
	}
	for _, pps := range self.PPS {
		n += 2 + len(pps)
	}
	return
}

func (self AVCDecoderConfRecord) Marshal(b []byte) (n int) {
	b[0] = 1
	b[1] = self.AVCProfileIndication
	b[2] = self.ProfileCompatibility
	b[3] = self.AVCLevelIndication
	b[4] = self.LengthSizeMinusOne | 0xfc
	b[5] = uint8(len(self.SPS)) | 0xe0
	n += 6

	for _, sps := range self.SPS {
		pio.PutU16BE(b[n:], uint16(len(sps)))
		n += 2
		copy(b[n:], sps)
		n += len(sps)
	}

	b[n] = uint8(len(self.PPS))
	n++

	for _, pps := range self.PPS {
		pio.PutU16BE(b[n:], uint16(len(pps)))
		n += 2
		copy(b[n:], pps)
		n += len(pps)
	}

	return
}

type SliceType uint

func (self SliceType) String() string {
	switch self {
	case SLICE_P:
		return "P"
	case SLICE_B:
		return "B"
	case SLICE_I:
		return "I"
	}
	return ""
}

const (
	SLICE_P = iota + 1
	SLICE_B
	SLICE_I
)

func ParseSliceHeaderFromNALU(packet []byte) (sliceType SliceType, err error) {

	if len(packet) <= 1 {
		err = fmt.Errorf("h264parser: packet too short to parse slice header")
		return
	}

	nal_unit_type := packet[0] & 0x1f
	switch nal_unit_type {
	case 1, 2, 5, 19:
		// slice_layer_without_partitioning_rbsp
		// slice_data_partition_a_layer_rbsp

	default:
		err = fmt.Errorf("h264parser: nal_unit_type=%d has no slice header", nal_unit_type)
		return
	}

	r := &bits.GolombBitReader{R: bytes.NewReader(packet[1:])}

	// first_mb_in_slice
	if _, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	// slice_type
	var u uint
	if u, err = r.ReadExponentialGolombCode(); err != nil {
		return
	}

	switch u {
	case 0, 3, 5, 8:
		sliceType = SLICE_P
	case 1, 6:
		sliceType = SLICE_B
	case 2, 4, 7, 9:
		sliceType = SLICE_I
	default:
		err = fmt.Errorf("h264parser: slice_type=%d invalid", u)
		return
	}

	return
}
