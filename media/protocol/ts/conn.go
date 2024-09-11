package ts

import (
	"net"

	"github.com/bugVanisher/streamer/media/protocol/common"
)

type Conn interface {
	Info() common.Info
	RemoteAddr() string
	Close() error
	Prepare() error
	NetConn() net.Conn
}
