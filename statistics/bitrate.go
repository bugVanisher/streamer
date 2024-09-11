package statistics

import (
	"fmt"
)

// Bitrate 码率统计对象
type Bitrate struct {
	statistic *PeriodicStatistic
}

// NewBitrate ...
func NewBitrate() *Bitrate {
	return &Bitrate{
		statistic: NewPeriodicStatistic(DefaultStatGridNum, 1),
	}
}

// Add ...
func (b *Bitrate) Add(size uint64) {
	b.statistic.Stat(int64(size))
}

// GetBitrate ...
func (b *Bitrate) GetBitrate() uint64 {
	return uint64(b.statistic.Avg())
}

// GetBitTotal ...
func (b *Bitrate) GetBitTotal() uint64 {
	return uint64(b.statistic.Sum())
}

func (b *Bitrate) String() string {
	return fmt.Sprintf("%dkb/s", b.statistic.Avg()/1024)
}
