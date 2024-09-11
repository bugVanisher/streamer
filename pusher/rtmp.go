package pusher

import (
	"context"
	"github.com/bugVanisher/streamer/common/errs"
	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/av/avutil"
	"github.com/bugVanisher/streamer/media/av/pktque"
	"github.com/bugVanisher/streamer/media/container/flv"
	"github.com/bugVanisher/streamer/media/protocol/rtmp"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"net/http"
	url2 "net/url"
	"path"
	"strings"
	"time"
)

type RtmpOverTcpUpStreamer struct {
	opt      []rtmp.Option
	rtmpUrl  string
	filename string
}

func NewRtmpPusher(rtmpUrl string, filename string, option ...rtmp.Option) *RtmpOverTcpUpStreamer {
	pusher := &RtmpOverTcpUpStreamer{
		rtmpUrl:  rtmpUrl,
		filename: filename,
		opt:      option,
	}
	return pusher
}

func init() {
	avutil.DefaultHandlers.Add(Handler)
}

func Handler(h *avutil.RegisterHandler) {
	h.Probe = func(b []byte) bool {
		return b[0] == 'F' && b[1] == 'L' && b[2] == 'V'
	}

	h.Ext = ".flv"

	h.ReaderDemuxer = func(r io.Reader) av.Demuxer {
		r_, ok := r.(io.ReadCloser)
		if !ok {
			panic("srt -> Handler -> ReaderDemuxer")
		}
		return flv.NewDemuxer(r_)
	}

	dialer := net.Dialer{}
	httpTransport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           dialer.DialContext,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	httpClient := &http.Client{
		Transport: httpTransport,
	}

	h.UrlDemuxer = func(s string) (bool, av.DemuxCloser, error) {
		if !strings.HasPrefix(s, "http") {
			return false, nil, nil
		}
		req, err := http.NewRequest("GET", s, nil)
		if err != nil {
			log.Error().Err(err).Str("url", s).Msg("[HTTPFLVIngester] prepare fail")
			return false, nil, errs.Wrapf(errs.ErrConnectURL, "url: %s", s)
		}

		req.Header.Set("User-Agent", "streamer")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Range", "bytes=0-")
		req.Header.Set("Connection", "close")

		response, err := httpClient.Do(req)
		if err != nil {
			log.Error().Err(err).Str("url", s).Msg("[HTTPFLVIngester] req fail")
			return false, nil, errs.Wrapf(errs.ErrConnectURL, "url: %s", s)
		}
		if response.StatusCode != http.StatusOK {
			return false, nil, errs.Wrapf(errs.ErrStreamNotExist, "url: %s", s)
		}
		return true, flv.NewDemuxer(response.Body), nil
	}

	h.WriterMuxer = func(w io.Writer) av.Muxer {
		return flv.NewMuxer(w)
	}

	h.CodecTypes = flv.CodecTypes
}

func (r *RtmpOverTcpUpStreamer) publish(ctx context.Context, url string, resource string) error {
	flvFile := resource
	rtmpURL := url

	isFile := path.IsAbs(flvFile)

	u, err := url2.Parse(rtmpURL)
	if err != nil {
		log.Error().Err(err).Msg("parse rtmp url error")
		return err
	}
	host := u.Host
	if !strings.Contains(u.Host, ":") {
		host = u.Host + ":1935"
	}
	r.opt = append([]rtmp.Option{rtmp.WithTcURL(rtmpURL)}, r.opt...)
	conn, err := rtmp.Dial(host, r.opt...)
	if err != nil {
		log.Error().Err(err).Msg("rtmp dial error")
		return err
	}
	defer conn.Close()

	err = conn.HandshakeClient()
	if err != nil {
		log.Error().Err(err).Msg("rtmp HandshakeClient error")
		return err
	}
	err = conn.ConnectPublish()
	if err != nil {
		log.Error().Err(err).Msg("rtmp ConnectPublish error")
		return err
	}

	pktCount := 0

	t := av.NewTransport(av.WithAfterWritePacket(func(pkt *av.Packet) error {
		pktCount++
		if pktCount%1000 == 0 {
			log.Debug().Msgf("send packet count %d", pktCount)
		}
		return nil
	}))

	round := 0

	filters := pktque.Filters{}
	if isFile {
		filters = append(filters, &pktque.FixTime{MakeIncrement: true}, &pktque.Walltime{})
	}
	var demuxer = &pktque.FilterDemuxer{Filter: filters}
	for {
		file, err := avutil.Open(flvFile)
		if err != nil {
			log.Error().Err(err).Msg("open file error")
			return err
		}
		demuxer.Demuxer = file
		err = t.CopyAV(ctx, conn, demuxer)
		if err != io.EOF {
			log.Error().Err(err).Msg("CopyAV error")
			return err
		}
		round++
		log.Debug().Msgf("has read %d round", round)
		err = file.Close()
		if err != nil {
			log.Error().Err(err).Msg("close file error")
			return err
		}
	}
}

func (r *RtmpOverTcpUpStreamer) Publish(ctx context.Context) error {
	return r.publish(ctx, r.rtmpUrl, r.filename)
}
