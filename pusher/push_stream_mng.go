package pusher

import (
	"context"
	"fmt"
	"github.com/bugVanisher/streamer/common/errs"
	"sync"
	"time"
)

type upStreamerManager struct {
	streams sync.Map
}

type upStreamInfo struct {
	pusher   Pusher
	duration time.Duration
	cancel   context.CancelFunc
}

var UpStreamerManager = &upStreamerManager{streams: sync.Map{}}

func Launch(name string, pusher Pusher, duration time.Duration) error {
	if _, ok := UpStreamerManager.streams.Load(name); ok {
		return errs.ErrDuplicateStream
	}
	ctx := context.Background()
	ctx, ctxCancel := context.WithTimeout(ctx, duration)
	UpStreamerManager.streams.Store(name, upStreamInfo{
		pusher:   pusher,
		duration: duration,
		cancel:   ctxCancel,
	})
	defer ctxCancel()
	// publish will block
	err := pusher.Publish(ctx)
	if _, ok := UpStreamerManager.streams.Load(name); ok {
		UpStreamerManager.streams.Delete(name)
	}
	if err != nil {
		return err
	}
	return nil
}

func Stop(name string) error {
	info, ok := UpStreamerManager.streams.Load(name)
	if !ok {
		return errs.ErrStreamNotExist
	}
	info.(upStreamInfo).cancel()
	return nil
}

func StopAll() {
	UpStreamerManager.streams.Range(func(key, value interface{}) bool {
		pushInfo := value.(upStreamInfo)
		pushInfo.cancel()
		return true
	})
}

func GetAllStreamInfos() (infos []string) {
	UpStreamerManager.streams.Range(func(key, value interface{}) bool {
		name := key.(string)
		pushInfo := value.(upStreamInfo)
		infos = append(infos, fmt.Sprintf("%s-%s", name, pushInfo.duration))
		return true
	})
	return infos
}
