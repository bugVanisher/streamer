package statistics

import (
	"fmt"
	"time"
)

const (
	DelayInterval = time.Second * 5
)

type Delay struct {
	// naloseconds
	delay    int64
	interval time.Duration

	beginTS    int64
	firstPktTS int64
}

func NewDelay() *Delay {
	return &Delay{
		interval: DelayInterval,
	}
}

func (d *Delay) Add(pktTS int64) {
	nowTS := time.Now().UnixNano()
	if d.beginTS == 0 {
		d.beginTS = nowTS
		d.firstPktTS = pktTS
	}

	wnd := nowTS - d.beginTS
	if wnd > int64(d.interval) {
		d.delay = wnd - (pktTS - d.firstPktTS)
		d.beginTS = nowTS
		d.firstPktTS = pktTS
	}
}

// return ms
func (d *Delay) GetDelay() int64 {
	// ns, us, ms
	return d.delay / 1000 / 1000
}

func (d *Delay) String() string {
	return fmt.Sprintf("%d ms", d.delay/1000/1000)
}
