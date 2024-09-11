package hls

import (
	"context"
	"github.com/rs/zerolog/log"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/bugVanisher/streamer/utils"
	jsoniter "github.com/json-iterator/go"
)

type HookEvent struct {
	Url  string
	Data interface{}
}

var (
	ctx        context.Context
	queue      chan *HookEvent
	once       sync.Once
	httpClient *http.Client
)

const (
	HookEventQueueLen  = 10000
	HookEventWorkerNum = 20
)

func InitHook(c context.Context) {
	ctx = c
	httpClient = createHTTPClient()
	queue = make(chan *HookEvent, HookEventQueueLen)
	for i := 0; i < HookEventWorkerNum; i++ {
		go run()
	}
}

func OnHookEvent(e *HookEvent) {
	if utils.ContextDone(ctx) {
		return
	}

	select {
	case queue <- e:
		return
	default:
		return
	}
}

func run() {
	defer utils.PanicRecover()
	defer func() {
		once.Do(func() {
			close(queue)
		})
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-queue:
			if !ok {
				return
			}
			err := handleHook(httpClient, e.Url, e.Data)
			if err != nil {
				log.Error().Err(err).Str("url", e.Url).Msg("[hls] handleHook fail")
			}
		default:
			time.Sleep(time.Millisecond * time.Duration(100))
		}
	}
}

func handleHook(cli *http.Client, url string, info interface{}) error {
	data, err := jsoniter.Marshal(info)
	if err != nil {
		return err
	}

	log.Info().Str("url", url).Str("data", string(data)).Msg("[hls] handleHook")
	_, err = utils.HTTPPost(cli, url, string(data))
	if err != nil {
		return err
	}
	return nil
}

func createHTTPClient() *http.Client {
	client := &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   3 * time.Second, // 连接超时时间
				KeepAlive: 3 * time.Second, // 发送keepalive报文的间隔时间
			}).DialContext,
			MaxIdleConns:          10,                      // 最大空闲连接数
			MaxIdleConnsPerHost:   10,                      // 每个host保持的空闲连接数
			MaxConnsPerHost:       10,                      // 每个host最大连接数
			IdleConnTimeout:       90 * time.Second,        // 空闲连接的超时时间, 超时自动关闭连接
			ExpectContinueTimeout: 1000 * time.Millisecond, // 等待服务第一个响应的超时时间
		},
		Timeout: 1000 * time.Millisecond,
	}
	return client
}

type HlsHookData struct {
	Action   string  `json:"action"`
	Ip       string  `json:"ip"`
	Vhost    string  `json:"vhost"`
	App      string  `json:"app"`
	Param    string  `json:"param"`
	Duration float32 `json:"duration"`
	File     string  `json:"file"`
	Url      string  `json:"url"`
	M3u8     string  `json:"m3u8"`
	M3u8Url  string  `json:"m3u8_url"`
	SeqNo    int     `json:"seq_no"`
}
