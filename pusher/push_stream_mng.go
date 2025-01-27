package pusher

import (
	"context"
	"errors"
	"github.com/bugVanisher/streamer/common/errs"
	"sync"
	"time"
)

type upStreamerManager struct {
	streams sync.Map
}

type upStreamInfo struct {
	pusher    Pusher
	duration  time.Duration
	cancel    context.CancelFunc
	startTime time.Time
}

type Expose struct {
	Name      string
	StartTime time.Time
	Dur       time.Duration
}

var UpStreamerManager = &upStreamerManager{streams: sync.Map{}}

func Launch(name string, pusher Pusher, duration time.Duration) error {
	if _, ok := UpStreamerManager.streams.Load(name); ok {
		return errs.ErrDuplicateStream
	}
	ctx := context.Background()
	ctx, ctxCancel := context.WithTimeout(ctx, duration)
	UpStreamerManager.streams.Store(name, upStreamInfo{
		pusher:    pusher,
		duration:  duration,
		cancel:    ctxCancel,
		startTime: time.Now(),
	})
	defer ctxCancel()
	// publish will block
	err := pusher.Publish(ctx)
	if _, ok := UpStreamerManager.streams.Load(name); ok {
		UpStreamerManager.streams.Delete(name)
	}
	if err != nil && !errors.Is(err, errs.ErrContextDone) {
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
	UpStreamerManager.streams.Delete(name)
	return nil
}

func StopAll() {
	UpStreamerManager.streams.Range(func(key, value interface{}) bool {
		pushInfo := value.(upStreamInfo)
		pushInfo.cancel()
		UpStreamerManager.streams.Delete(key.(string))
		return true
	})
}

func GetAllStreamInfos() (infos []Expose) {
	UpStreamerManager.streams.Range(func(key, value interface{}) bool {
		name := key.(string)
		pushInfo := value.(upStreamInfo)
		infos = append(infos, Expose{
			Name:      name,
			StartTime: pushInfo.startTime,
			Dur:       pushInfo.duration,
		})
		return true
	})
	return infos
}
