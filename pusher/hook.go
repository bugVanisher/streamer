package pusher

import "github.com/bugVanisher/streamer/protocol/common"

type Hook interface {
	OnPlayOrPublish(info common.Info) error
}
