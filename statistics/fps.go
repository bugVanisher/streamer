package statistics

import (
	"fmt"
	"time"
)

// FPS 帧率统计
type FPS struct {
	fps      uint32
	interval time.Duration

	frameCount int64
	beginTS    int64
}

// NewFPS 创建FPS实例
func NewFPS() *FPS {
	return &FPS{
		interval: time.Second,
	}
}

// Add ...
func (f *FPS) Add() {
	nowTS := time.Now().UnixNano()

	f.frameCount++
	d := nowTS - f.beginTS
	if d >= int64(f.interval) {
		f.fps = uint32(f.frameCount * int64(time.Second) / d)
		f.frameCount = 0
		f.beginTS = nowTS
	}

}

// GetFPS ...
func (f *FPS) GetFPS() uint32 {
	return f.fps
}

func (f *FPS) String() string {
	return fmt.Sprintf("%d", f.fps)
}
