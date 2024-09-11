package statistics

import "time"

type Duration struct {
	duration          int64
	lastPktTs         int64 //nanosecond
	maxPacketDuration int64
}

func NewDuration() *Duration {
	return &Duration{
		maxPacketDuration: int64(100 * time.Millisecond),
	}
}

func (d *Duration) Add(pktTS int64) {
	if d.lastPktTs == 0 {
		d.lastPktTs = pktTS
		//d.duration += int64(1 * time.Millisecond)
		return
	}
	if pktTS <= d.lastPktTs {
		d.lastPktTs = pktTS
	} else if pktTS-d.lastPktTs > d.maxPacketDuration {
		d.duration += d.maxPacketDuration
		d.lastPktTs = pktTS
	} else {
		d.duration += pktTS - d.lastPktTs
		d.lastPktTs = pktTS
	}
}

// GetDuration only call by stat once every period
func (d *Duration) GetDuration() int64 {
	tmp := d.duration
	d.duration = 0
	return tmp
}
