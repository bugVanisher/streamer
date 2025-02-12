package downstream

import (
	"context"
	"crypto/tls"
	"errors"
	"github.com/bugVanisher/streamer/common/errs"
	"github.com/bugVanisher/streamer/media/av"
	flvm "github.com/bugVanisher/streamer/media/container/flv"
	"github.com/bugVanisher/streamer/media/protocol/flv"
	"github.com/bugVanisher/streamer/statistics"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"net/http"
	"time"
)

type FlvDownStreamer struct {
	Url       string
	Writer    io.Writer
	avFlow    *statistics.AVFlow
	width     uint32
	height    uint32
	firstPkt  bool
	codecType av.CodecType
	options   *flv.Options
}

func NewFlvDownStreamer(url string, writer io.Writer, options ...flv.Option) *FlvDownStreamer {
	flvDownStreamer := &FlvDownStreamer{
		Url:    url,
		Writer: writer,
	}
	opts := flv.DefaultOptions
	for _, o := range options {
		o(&opts)
	}
	flvDownStreamer.options = &opts
	return flvDownStreamer
}

func (d *FlvDownStreamer) Pull(ctx context.Context) (bool, error) {
	dialer := net.Dialer{}
	httpTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // 忽略证书验证
		},
	}
	httpClient := &http.Client{
		Transport: httpTransport,
	}

	req, err := http.NewRequest("GET", d.Url, nil)
	if err != nil {
		log.Error().Err(err).Str("url", d.Url).Msg("[HTTPFLVIngester] prepare fail")
		return false, errs.Wrapf(errs.ErrConnectURL, "url: %s", d.Url)
	}

	req.Header.Set("User-Agent", "streamer")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Range", "bytes=0-")
	req.Header.Set("Connection", "close")

	response, err := httpClient.Do(req)
	if err != nil {
		log.Error().Err(err).Str("url", d.Url).Msg("[HTTPFLVIngester] req fail")
		return false, errs.Wrapf(errs.ErrConnectURL, "url: %s", d.Url)
	}
	if response.StatusCode != http.StatusOK {
		return false, errs.Wrapf(errs.ErrStreamNotExist, "url: %s", d.Url)
	}
	pktCount := 0
	d.avFlow = statistics.NewAVFlow()
	t := av.NewTransport(av.WithAfterReadPacket(func(pkt *av.Packet) error {
		d.avFlow.Stat(pkt)
		pktCount++
		if pktCount%1000 == 0 {
			log.Debug().Msgf("recv packet count %d\n", pktCount)
		}
		return nil
	}), av.WithAfterReadHeaders(d.AfterReadHeader))
	muxer := flvm.NewMuxer(d.Writer)
	stop := make(chan bool)
	go d.LogStatistic(stop)
	err = t.CopyAV(ctx, muxer, flvm.NewDemuxer(response.Body))
	stop <- true
	if err != nil && !errors.Is(err, errs.ErrContextDone) {
		log.Error().Err(err).Msg("CopyAV error")
		return false, errs.Wrapf(errs.ErrConnectURL, "url: %s", d.Url)
	}
	return true, nil
}

func (d *FlvDownStreamer) LogStatistic(done chan bool) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	time.Sleep(2 * time.Second)
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			stat := statistics.StreamHandler{
				VideoBitrate:  d.avFlow.VideoBitrate.GetBitrate(),
				VideoFPS:      d.avFlow.VideoFPS.GetFPS(),
				AudioFPS:      d.avFlow.AudioFPS.GetFPS(),
				VideoGop:      d.avFlow.VideoGop.GetGop(),
				VideoDuration: d.avFlow.VideoDuration.GetDuration(),
				AudioDuration: d.avFlow.AudioDuration.GetDuration(),
				AudioBitrate:  d.avFlow.AudioBitrate.GetBitrate(),
				VideoWidth:    d.width,
				VideoHeight:   d.height,
				VideoDelay:    d.avFlow.VideoDelay.GetDelay(),
				CodecType:     d.codecType.String(),
			}
			if d.options.StatisticHook != nil {
				d.options.StatisticHook.OnStatisticStat(stat)
			}
		}
	}

}

func (d *FlvDownStreamer) AfterReadHeader(data []av.CodecData) error {
	if !d.firstPkt {
		log.Info().Msg("[HTTPFLVIngester]read first header")
	}
	for _, codec := range data {
		if codec.Type().IsVideo() {
			d.codecType = codec.Type()
			data := codec.(av.VideoCodecData)
			d.width = uint32(data.Width())
			d.height = uint32(data.Height())
			break
		}
	}
	return nil
}
