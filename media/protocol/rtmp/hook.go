package rtmp

import "github.com/bugVanisher/streamer/media/protocol/common"

type Hook interface {
	OnPlayOrPublish(info common.Info) error
}
