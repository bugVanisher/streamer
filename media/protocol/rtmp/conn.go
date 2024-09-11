package rtmp

import (
	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"github.com/bugVanisher/streamer/media/protocol/common"
)

// Conn 包装了rtmp协议的基础接口
type Conn interface {
	av.MuxCloser
	av.Demuxer

	ReadConnect() error
	RemoteAddr() string
	Info() common.Info

	HandshakeClient() error
	ConnectPublish() error // 执行connect命令和publish命令
	ConnectPlay() error    // 执行connect命令和play命令

	OnStatus(msg flvio.AMFMap) error
	HandshakeServer() error
	VideoResolution() (width uint32, height uint32)
	ProtoType() string
}
