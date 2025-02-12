package statistics

import (
	"fmt"
	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"time"
)

const StatInterval = 3 * time.Second

// AVFlow 流统计
type AVFlow struct {
	VideoBitrate  *Bitrate
	AudioBitrate  *Bitrate
	VideoFPS      *FPS
	AudioFPS      *FPS
	VideoGop      *Gop
	VideoDelay    *Delay
	VideoDuration *Duration
	AudioDuration *Duration
}

// NewAVFlow 创建AVFlow实例
func NewAVFlow() *AVFlow {
	return &AVFlow{
		VideoBitrate:  NewBitrate(),
		AudioBitrate:  NewBitrate(),
		VideoFPS:      NewFPS(),
		AudioFPS:      NewFPS(),
		VideoGop:      NewGop(),
		VideoDelay:    NewDelay(),
		VideoDuration: NewDuration(),
		AudioDuration: NewDuration(),
	}
}

// Stat 统计av.Packet的音视频数据
func (s *AVFlow) Stat(pkt *av.Packet) {
	if pkt.DataType == flvio.TAG_VIDEO {
		s.VideoBitrate.Add(uint64(len(pkt.Data) * 8)) //bit
		s.VideoFPS.Add()
		s.VideoGop.Add(pkt)
		s.VideoDelay.Add(int64(pkt.Time))
		s.VideoDuration.Add(int64(pkt.Time))
	} else if pkt.DataType == flvio.TAG_AUDIO {
		s.AudioFPS.Add()
		s.AudioBitrate.Add(uint64(len(pkt.Data) * 8)) //bit
		s.AudioDuration.Add(int64(pkt.Time))
	}
}

type Stream struct {
	PlayerNum      int32
	QuicPlayerNum  int32
	HlsPlayerNum   int32
	SlicePlayerNum int32
	ForwarderNum   int32
	InBW           uint64
	OutBW          uint64
	HlsBw          uint64
	SliceInBW      uint64
	SliceOutBW     uint64
}

type StreamHandler struct {
	VideoBitrate  uint64
	VideoFPS      uint32
	AudioFPS      uint32
	VideoGop      float64
	VideoWidth    uint32
	VideoHeight   uint32
	VideoDuration int64
	AudioDuration int64
	AudioBitrate  uint64
	VideoDelay    int64
	CodecType     string
}

// VideoDurationDelay 视频时长与现实时间的diff，毫秒
func (s *StreamHandler) VideoDurationDelay() int64 {
	return (int64(StatInterval) - s.VideoDuration) / int64(time.Millisecond)
}

func (s *StreamHandler) String() string {
	return fmt.Sprintf(
		"CodecType: %s, Video Bitrate: %.2f kbps, Audio Bitrate: %.2f kbps, Video FPS: %d, Audio FPS: %d, Video GOP: %.2f, Resolution: %dx%d, Video Duration: %d ms, Audio Duration: %d ms, Video Delay: %d ms",
		s.CodecType,
		float64(s.VideoBitrate)/1000,
		float64(s.AudioBitrate)/1000,
		s.VideoFPS,
		s.AudioFPS,
		s.VideoGop,
		s.VideoWidth,
		s.VideoHeight,
		s.VideoDuration/1e6, // 假设 VideoDuration 是纳秒
		s.AudioDuration/1e6, // 假设 AudioDuration 是纳秒
		s.VideoDelay/1e6,    // 假设 VideoDelay 是纳秒
	)
}
