package downstream

import (
	"context"
	"github.com/bugVanisher/streamer/common/errs"
	"sync"
	"time"
)

type downStreamerManager struct {
	streams sync.Map
}

type downStreamInfo struct {
	downStreamer DownStreamer
	duration     time.Duration
	cancel       context.CancelFunc
}

var DownStreamerManager = &downStreamerManager{streams: sync.Map{}}

func Launch(name string, downStreamer DownStreamer, duration time.Duration) error {
	if _, ok := DownStreamerManager.streams.Load(name); ok {
		return errs.ErrDuplicateStream
	}
	ctx := context.Background()
	ctx, ctxCancel := context.WithTimeout(ctx, duration)
	DownStreamerManager.streams.Store(name, downStreamInfo{
		downStreamer: downStreamer,
		duration:     duration,
		cancel:       ctxCancel,
	})
	defer ctxCancel()
	// Pull will block
	_, err := downStreamer.Pull(ctx)
	if _, ok := DownStreamerManager.streams.Load(name); ok {
		DownStreamerManager.streams.Delete(name)
	}
	if err != nil {
		return err
	}
	return nil
}

func Stop(name string) error {
	info, ok := DownStreamerManager.streams.Load(name)
	if !ok {
		return errs.ErrStreamNotExist
	}
	info.(downStreamInfo).cancel()
	return nil
}
