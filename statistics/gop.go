package statistics

import (
	"fmt"
	"github.com/bugVanisher/streamer/media/av"
)

// Gop gop统计
type Gop struct {
	// naloseconds
	gop int64

	lastKeyPktTS int64
}

// NewGop ...
func NewGop() *Gop {
	return &Gop{}
}

// Add ...
func (g *Gop) Add(pkt *av.Packet) {
	if pkt.IsKeyFrame {
		g.gop = int64(pkt.Time) - g.lastKeyPktTS
		g.lastKeyPktTS = int64(pkt.Time)
	}
}

// GetGop ...
func (g *Gop) GetGop() float64 {
	if g.gop == g.lastKeyPktTS {
		return 0
	}
	// ns, us, ms
	return float64(g.gop) / 1000 / 1000 / 1000
}

func (g *Gop) String() string {
	return fmt.Sprintf("%.2f s", g.GetGop())
}
