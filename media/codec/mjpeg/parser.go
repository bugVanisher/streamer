package mjpeg

import "github.com/bugVanisher/streamer/media/av"

type CodecData struct {
}

func (d CodecData) Type() av.CodecType {
	return av.MJPEG
}
