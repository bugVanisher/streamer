package rtmp

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/av/avutil"
	h264parser "github.com/bugVanisher/streamer/media/codec/h264parser"
	"github.com/bugVanisher/streamer/media/container/flv"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
	"github.com/bugVanisher/streamer/media/protocol/common"
	"github.com/bugVanisher/streamer/utils"
	"github.com/bugVanisher/streamer/utils/bits/pio"
	"github.com/pkg/errors"
)

var Debug bool

const (
	stageHandshakeDone = iota + 1
	stageCommandDone
	stageCodecDataDone
)

const (
	prepareReading = iota + 1
	prepareWriting
)

type conn struct {
	URL  *url.URL
	info common.Info

	prober                         *flv.Prober
	streams                        []av.CodecData
	VideoStreamIdx, AudioStreamIdx int

	txbytes uint64
	rxbytes uint64

	bufr *bufio.Reader
	bufw *bufio.Writer
	ackn uint32

	writebuf []byte
	readbuf  []byte

	netconn   net.Conn
	txrxcount *txrxcount

	writeMaxChunkSize int
	readMaxChunkSize  int
	readAckSize       uint32
	readcsmap         map[uint32]*chunkStream

	publishing, playing bool
	reading, writing    bool
	stage               int

	avmsgsid uint32

	gotcommand     bool
	commandname    string
	commandtransid float64
	commandobj     flvio.AMFMap
	commandparams  []interface{}

	gotmsg      bool
	timestamp   uint32
	msgdata     []byte
	msgtypeid   uint8
	datamsgvals []interface{}
	avtag       flvio.Tag
	scripttag   flvio.Tag

	eventtype uint16
	debuger   *Debuger

	opts *Options
}

func (self *conn) Streams() (streams []av.CodecData, err error) {
	if err = self.prepare(stageCodecDataDone, prepareReading); err != nil {
		return
	}
	streams = self.streams
	return
}

type txrxcount struct {
	io.ReadWriter
	txbytes uint64
	rxbytes uint64
}

func (self *txrxcount) Read(p []byte) (int, error) {
	n, err := self.ReadWriter.Read(p)
	self.rxbytes += uint64(n)
	return n, err
}

func (self *txrxcount) Write(p []byte) (int, error) {
	n, err := self.ReadWriter.Write(p)
	self.txbytes += uint64(n)
	return n, err
}

func NewConn(netconn net.Conn, opt ...Option) Conn {
	return newConn(netconn, opt...)
}

func Dial(host string, opt ...Option) (conn Conn, err error) {
	opts := DefaultOptions
	for _, o := range opt {
		o(&opts)
	}
	opts.IsServer = false

	var netConn net.Conn
	if netConn, err = net.DialTimeout("tcp", host, opts.DialTimeout); err != nil {
		return
	}

	c := newConn(netConn, opt...)

	tcURL, host, app, streamID, err := ParseURLDetail(opts.TcURL)
	if err != nil {
		return
	}
	c.URL = tcURL
	c.info.App = app
	c.info.StreamName = streamID
	c.info.ID = utils.ExtractStreamID(streamID)
	c.info.Domain = host
	c.info.RawURL = opts.TcURL
	c.prober.TaskID = streamID

	return c, nil
}

func newConn(netconn net.Conn, opt ...Option) *conn {
	conn := &conn{}

	opts := DefaultOptions
	for _, o := range opt {
		o(&opts)
	}
	conn.opts = &opts

	conn.prober = &flv.Prober{}
	conn.netconn = netconn
	conn.readcsmap = make(map[uint32]*chunkStream)
	conn.readMaxChunkSize = 128
	conn.writeMaxChunkSize = 128
	conn.bufr = bufio.NewReaderSize(netconn, conn.opts.ReadBufferSize)
	conn.bufw = bufio.NewWriterSize(netconn, conn.opts.WriteBufferSize)
	conn.txrxcount = &txrxcount{ReadWriter: netconn}
	conn.writebuf = make([]byte, 4096)
	conn.readbuf = make([]byte, 4096)

	if conn.opts.EnableDebug {
		conn.debuger = NewDebuger(conn.opts.RoleID)
		logFile := fmt.Sprintf("../../../log/rtmpdebug.%s.log", conn.opts.RoleID)
		conn.debuger.StartDebug(logFile, -1)
	}

	return conn
}

type chunkStream struct {
	timenow     uint32
	timedelta   uint32
	hastimeext  bool
	msgsid      uint32
	msgtypeid   uint8
	msgdatalen  uint32
	msgdataleft uint32
	msghdrtype  uint8
	msgdata     []byte
}

func (self *chunkStream) Start() {
	self.msgdataleft = self.msgdatalen
	self.msgdata = make([]byte, self.msgdatalen)
}

const (
	msgtypeidUserControl      = 4
	msgtypeidAck              = 3
	msgtypeidWindowAckSize    = 5
	msgtypeidSetPeerBandwidth = 6
	msgtypeidSetChunkSize     = 1
	msgtypeidCommandMsgAMF0   = 20
	msgtypeidCommandMsgAMF3   = 17
	msgtypeidDataMsgAMF0      = 18
	msgtypeidDataMsgAMF3      = 15
	msgtypeidVideoMsg         = 9
	msgtypeidAudioMsg         = 8
)

const (
	eventtypeStreamBegin      = 0
	eventtypeSetBufferLength  = 3
	eventtypeStreamIsRecorded = 4
)

func (self *conn) ProtoType() string {
	return "rtmp"
}

func (self *conn) NetConn() net.Conn {
	return self.netconn
}

func (self *conn) TxBytes() uint64 {
	return self.txrxcount.txbytes
}

func (self *conn) RxBytes() uint64 {
	return self.txrxcount.rxbytes
}

func (self *conn) Close() (err error) {
	if self.netconn != nil {
		return self.netconn.Close()
	}
	return nil
}

func (self *conn) pollCommand() (err error) {
	for {
		if err = self.pollMsg(); err != nil {
			return
		}
		if self.gotcommand {
			return
		}
	}
}

func (self *conn) pollAVTag() (tag flvio.Tag, err error) {
	for {
		if err = self.pollMsg(); err != nil {
			return
		}
		switch self.msgtypeid {
		case msgtypeidVideoMsg, msgtypeidAudioMsg:
			tag = self.avtag
			return
		case msgtypeidDataMsgAMF0, msgtypeidDataMsgAMF3:
			if Debug {
				log.Debug().Str("taskid", self.prober.TaskID).Uint8("msgtypeid", self.msgtypeid).Any("tag", self.scripttag).Msg("pollAVTag: unhandled msg: scripttag used to trigger header")
			}
			tag = self.scripttag
			return
		default:
			if Debug {
				log.Debug().Str("taskid", self.prober.TaskID).Uint8("msgtypeid", self.msgtypeid).Str("commandname", self.commandname).Msg("pollAVTag: unhandled msg")
			}
		}
	}
}

func (self *conn) pollMsg() (err error) {
	self.gotmsg = false
	self.gotcommand = false
	self.datamsgvals = nil
	self.avtag = flvio.Tag{}
	self.scripttag = flvio.Tag{}
	for {
		if err = self.readChunk(); err != nil {
			return
		}
		if self.gotmsg {
			return
		}
	}
}

func SplitPath(u *url.URL) (app, stream string) {
	pathsegs := strings.Split(u.RequestURI(), "/")
	if len(pathsegs) == 3 { // e.g. /app/stream
		app = pathsegs[1]
		stream = pathsegs[2]
	} else if len(pathsegs) == 4 { // e.g. /host/app/stream
		app = pathsegs[2]
		stream = pathsegs[3]
	}
	return
}

func getTcUrl(u *url.URL) string {
	app, _ := SplitPath(u)
	nu := *u
	nu.Path = "/" + app
	nu.RawQuery = ""
	nu.ForceQuery = false
	return nu.String()
}

func createURL(tcurl, cpath, ppath string, srt bool) (u *url.URL, info common.Info, err error) {
	ppathSlice := strings.Split(strings.TrimSpace(ppath), "\\")
	if len(ppathSlice) > 1 {
		ppath = ppathSlice[0]
	}
	ps := strings.Split(cpath+"/"+ppath, "/")
	out := []string{""}
	for _, s := range ps {
		if len(s) > 0 {
			out = append(out, s)
		}
	}
	if len(out) < 2 {
		out = append(out, "")
	}
	path := strings.Join(out, "/")

	u, err = url.ParseRequestURI(path)
	if err != nil {
		return
	}

	if tcurl != "" {
		tu, err := url.Parse(tcurl)
		if err == nil && tu != nil {
			info.Domain = tu.Host
			u.Host = tu.Host
			u.Scheme = tu.Scheme
		}
	}

	if srt {
		u.Scheme = "srt"
	}

	domain, app := extractDomainAndApp(cpath)
	host := u.Query().Get("speHost")
	if host != "" {
		info.Domain = host
	} else if domain != "" {
		info.Domain = domain
	}
	info.Domain = utils.PeelOffPort1935(info.Domain)
	info.App = app
	info.StreamName = resolveStreamID(ppath)
	info.ID = utils.ExtractStreamID(info.StreamName)
	info.RawURL = u.String()
	return
}

func extractDomainAndApp(cpath string) (domain string, app string) {
	ss := strings.Split(cpath, "/")
	var cpathSlice []string
	for _, s := range ss {
		if len(s) > 0 {
			cpathSlice = append(cpathSlice, s)
		}
	}

	if len(cpathSlice) == 2 {
		domain = cpathSlice[0]
		app = cpathSlice[1]
		return
	}
	app = cpath
	return
}

var CodecTypes = flv.CodecTypes

func (self *conn) writeBasicConf() (err error) {
	// > SetChunkSize
	if err = self.writeSetChunkSize(self.opts.ChunkSize); err != nil {
		return
	}
	// > WindowAckSize
	if err = self.writeWindowAckSize(5000000); err != nil {
		return
	}
	// > SetPeerBandwidth
	if err = self.writeSetPeerBandwidth(5000000, 2); err != nil {
		return
	}
	return
}

func (self *conn) ReadConnect() (err error) {
	var connectpath string

	// < connect("app")
	if err = self.pollCommand(); err != nil {
		return
	}
	if self.commandname != "connect" {
		err = fmt.Errorf("rtmp: first command is not connect")
		return
	}
	if self.commandobj == nil {
		err = fmt.Errorf("rtmp: connect command params invalid")
		return
	}

	var ok bool
	var _app, _tcurl interface{}
	if _app, ok = self.commandobj["app"]; !ok {
		err = fmt.Errorf("rtmp: `connect` params missing `app`")
		return
	}
	connectpath, _ = _app.(string)

	var tcurl string
	if _tcurl, ok = self.commandobj["tcUrl"]; !ok {
		_tcurl, ok = self.commandobj["tcurl"]
	}
	if ok {
		tcurl, _ = _tcurl.(string)
	}

	log.Info().Msg(fmt.Sprintf("[rtmp] < connect(%s) tcurl=%s timeout=%v", connectpath, tcurl, self.opts.ReadWriteTimeout))

	if err = self.writeBasicConf(); err != nil {
		return
	}

	// > _result("NetConnection.Connect.Success")
	if err = self.writeCommandMsg(3, 0, "_result", self.commandtransid,
		flvio.AMFMap{
			"fmtVer":       "FMS/3,0,1,123",
			"capabilities": 31,
		},
		flvio.AMFMap{
			"level":          "status",
			"code":           "NetConnection.Connect.Success",
			"description":    "Connection succeeded.",
			"objectEncoding": 3,
		},
	); err != nil {
		return
	}

	if err = self.flushWrite(); err != nil {
		return
	}

	for {
		if err = self.pollMsg(); err != nil {
			return
		}
		if self.gotcommand {
			switch self.commandname {

			// < createStream
			case "createStream":
				self.avmsgsid = uint32(1)
				// > _result(streamid)
				if err = self.writeCommandMsg(3, 0, "_result", self.commandtransid, nil, self.avmsgsid); err != nil {
					return
				}
				if err = self.flushWrite(); err != nil {
					return
				}

			// < publish("path")
			case "publish":
				if len(self.commandparams) < 1 {
					err = fmt.Errorf("rtmp: publish params invalid")
					return
				}
				publishpath, _ := self.commandparams[0].(string)

				log.Info().Msg(fmt.Sprintf("[rtmp] < publish(%s)", publishpath))

				self.URL, self.info, err = createURL(tcurl, connectpath, publishpath, ok)
				if err != nil {
					err = fmt.Errorf("rtmp: publish params wrong: %v", err)
					return
				}

				onStatusMsg := AMFMapOnStatusPublishStart
				var cberr error
				if self.opts.Hook != nil {
					cberr = self.opts.Hook.OnPlayOrPublish(self.info)
					if cberr != nil {
						onStatusMsg = AMFMapOnStatusPublishStreamDuplicated
					}
				}

				// > onStatus()
				if err = self.writeCommandMsg(5, self.avmsgsid, "onStatus", self.commandtransid, nil, onStatusMsg); err != nil {
					return
				}
				if err = self.flushWrite(); err != nil {
					return
				}

				if cberr != nil {
					err = fmt.Errorf("rtmp: OnPlayOrPublish check error: %v", cberr)
					return
				}

				self.publishing = true
				self.reading = true
				self.prober.TaskID = self.info.StreamName
				self.stage++
				return

			// < play("path")
			case "play":
				if len(self.commandparams) < 1 {
					err = fmt.Errorf("rtmp: play params invalid")
					return
				}
				playpath, _ := self.commandparams[0].(string)

				log.Debug().Msg(fmt.Sprintf("[rtmp] < play(%s)", playpath))

				self.URL, self.info, err = createURL(tcurl, connectpath, playpath, false)
				if err != nil {
					err = fmt.Errorf("rtmp: play params wrong: %v", err)
					return
				}

				// > streamBegin(streamid)
				if err = self.writeStreamBegin(self.avmsgsid); err != nil {
					return
				}

				// > onStatus()
				if err = self.writeCommandMsg(5, self.avmsgsid,
					"onStatus", self.commandtransid, nil,
					flvio.AMFMap{
						"level":       "status",
						"code":        "NetStream.Play.Start",
						"description": "Start live",
					},
				); err != nil {
					return
				}

				// > |RtmpSampleAccess()
				if err = self.writeDataMsg(5, self.avmsgsid,
					"|RtmpSampleAccess", true, true,
				); err != nil {
					return
				}

				if err = self.flushWrite(); err != nil {
					return
				}

				self.playing = true
				self.writing = true
				self.prober.TaskID = self.info.StreamName
				self.stage++
				return
			}
		}
	}
	return
}

func (self *conn) OnStatus(msg flvio.AMFMap) error {
	if self.publishing {
		return self.publishOnStatus(msg)
	} else if self.playing {
		return self.playOnStatus(msg)
	}
	return nil
}

func (self *conn) playOnStatus(msg flvio.AMFMap) error {
	return nil
}

func (self *conn) publishOnStatus(msg flvio.AMFMap) error {
	// > onStatus()
	if err := self.writeCommandMsg(5, self.avmsgsid,
		"onStatus", self.commandtransid, nil, msg,
	); err != nil {
		return err
	}
	if err := self.flushWrite(); err != nil {
		return err
	}
	return nil
}

func (self *conn) checkConnectResult() (ok bool, errmsg string) {
	if len(self.commandparams) < 1 {
		errmsg = "checkConnectResult: params length < 1"
		return
	}

	obj, _ := self.commandparams[0].(flvio.AMFMap)
	if obj == nil {
		errmsg = "checkConnectResult: params[0] not object"
		return
	}

	_code, _ := obj["code"]
	if _code == nil {
		errmsg = "checkConnectResult: code invalid"
		return
	}

	code, _ := _code.(string)
	if code != "NetConnection.Connect.Success" {
		errmsg = "checkConnectResult: code != NetConnection.Connect.Success"
		return
	}

	ok = true
	return
}

func (self *conn) checkCreateStreamResult() (ok bool, avmsgsid uint32) {
	if len(self.commandparams) < 1 {
		return
	}

	ok = true
	_avmsgsid, _ := self.commandparams[0].(float64)
	avmsgsid = uint32(_avmsgsid)
	return
}

func (self *conn) checkPublishResult() error {
	if len(self.commandparams) < 1 {
		return fmt.Errorf("rtmp: publish result error: params length < 1")
	}

	obj, _ := self.commandparams[0].(flvio.AMFMap)
	if obj == nil {
		return fmt.Errorf("rtmp: publish result error: params[0] not object")
	}

	_code, _ := obj["code"]
	if _code == nil {
		return fmt.Errorf("rtmp: publish result error: code invalid")
	}

	code, _ := _code.(string)
	if code != "NetStream.Publish.Start" {
		if code == "NetStream.Publish.StreamDuplicated" {
			return errors.New("StreamDuplicated")
		}
		return fmt.Errorf("rtmp: publish result error: code == %s", code)
	}

	return nil
}

func (self *conn) probe() (err error) {
	for !self.prober.Probed() {
		var tag flvio.Tag
		if tag, err = self.pollAVTag(); err != nil {
			return
		}
		if err = self.prober.PushTag(tag, int32(self.timestamp)); err != nil {
			return
		}
	}

	self.streams = self.prober.Streams
	self.stage++
	return
}

func (self *conn) writeConnect(path string) (err error) {
	if err = self.writeBasicConf(); err != nil {
		return
	}

	// > connect("app")
	log.Debug().Msg(fmt.Sprintf("[rtmp] > connect('%s') host=%s", path, self.URL.Host))

	if err = self.writeCommandMsg(3, 0, "connect", 1,
		flvio.AMFMap{
			"app":           path,
			"flashVer":      "MAC 22,0,0,192",
			"tcUrl":         getTcUrl(self.URL),
			"fpad":          false,
			"capabilities":  15,
			"audioCodecs":   4071,
			"videoCodecs":   252,
			"videoFunction": 1,
		},
	); err != nil {
		return
	}

	if err = self.flushWrite(); err != nil {
		return
	}

	for {
		if err = self.pollMsg(); err != nil {
			return
		}
		if self.gotcommand {
			// < _result("NetConnection.Connect.Success")
			if self.commandname == "_result" {
				var ok bool
				var errmsg string
				if ok, errmsg = self.checkConnectResult(); !ok {
					err = fmt.Errorf("rtmp: command connect failed: %s", errmsg)
					return
				}
				if Debug {
					log.Debug().Str("ID", self.Info().ID).Msg("[rtmp] < _result() of connect")
				}
				break
			}
		} else {
			if self.msgtypeid == msgtypeidWindowAckSize {
				if len(self.msgdata) == 4 {
					self.readAckSize = pio.U32BE(self.msgdata)
				}
				if err = self.writeWindowAckSize(0xffffffff); err != nil {
					return
				}
			}
		}
	}

	return
}

func (self *conn) connectPublish() (err error) {
	connectpath, publishpath := SplitPath(self.URL)

	if err = self.writeConnect(connectpath); err != nil {
		err = errors.Wrap(err, "rtmp: connectPublish failed")
		return
	}

	transid := 2

	// > createStream()
	log.Debug().Str("ID", self.Info().ID).Msg("[rtmp] > createStream()")

	if err = self.writeCommandMsg(3, 0, "createStream", transid, nil); err != nil {
		err = errors.Wrap(err, "rtmp: connectPublish failed")
		return
	}
	transid++

	if err = self.flushWrite(); err != nil {
		err = errors.Wrap(err, "rtmp: connectPublish failed")
		return
	}

	for {
		if err = self.pollMsg(); err != nil {
			err = errors.Wrap(err, "rtmp: connectPublish failed")
			return
		}
		if self.gotcommand {
			// < _result(avmsgsid) of createStream
			if self.commandname == "_result" {
				var ok bool
				if ok, self.avmsgsid = self.checkCreateStreamResult(); !ok {
					err = fmt.Errorf("rtmp: createStream command failed")
					return
				}
				break
			}
		}
	}

	// > publish('app')
	log.Debug().Str("ID", self.Info().ID).Msgf(fmt.Sprintf("[rtmp] > publish('%s')", publishpath))

	if err = self.writeCommandMsg(8, self.avmsgsid, "publish", transid, nil, publishpath); err != nil {
		err = errors.Wrap(err, "rtmp: connectPublish failed")
		return
	}
	transid++

	if err = self.flushWrite(); err != nil {
		err = errors.Wrap(err, "rtmp: connectPublish failed")
		return
	}

	for {
		if err = self.pollMsg(); err != nil {
			err = errors.Wrap(err, "rtmp: connectPublish failed")
			return
		}

		if self.gotcommand {
			// < onStatus() of publish
			if self.commandname == "onStatus" {
				if err = self.checkPublishResult(); err != nil {
					return
				}
				break
			}
		}
	}

	self.writing = true
	self.publishing = true
	self.stage++
	return
}

func (self *conn) connectPlay() (err error) {
	connectpath, playpath := SplitPath(self.URL)

	if err = self.writeConnect(connectpath); err != nil {
		return
	}

	// > createStream()
	log.Debug().Str("ID", self.Info().ID).Msg("[rtmp] > createStream()")

	if err = self.writeCommandMsg(3, 0, "createStream", 2, nil); err != nil {
		return
	}

	// > SetBufferLength 0,100ms
	if err = self.writeSetBufferLength(0, 100); err != nil {
		return
	}

	if err = self.flushWrite(); err != nil {
		return
	}

	for {
		if err = self.pollMsg(); err != nil {
			return
		}
		if self.gotcommand {
			// < _result(avmsgsid) of createStream
			if self.commandname == "_result" {
				var ok bool
				if ok, self.avmsgsid = self.checkCreateStreamResult(); !ok {
					err = fmt.Errorf("rtmp: createStream command failed")
					return
				}
				break
			}
		}
	}

	// > play('app')
	log.Debug().Msg(fmt.Sprintf("[rtmp] > play('%s')", playpath))

	if err = self.writeCommandMsg(8, self.avmsgsid, "play", 0, nil, playpath); err != nil {
		return
	}
	if err = self.flushWrite(); err != nil {
		return
	}

	self.reading = true
	self.playing = true
	self.stage++
	return
}

func (self *conn) ReadPacket() (pkt av.Packet, err error) {
	if err = self.prepare(stageCodecDataDone, prepareReading); err != nil {
		return
	}

	if !self.prober.Empty() {
		pkt = self.prober.PopPacket()
		return
	}

	for {
		var tag flvio.Tag
		if tag, err = self.pollAVTag(); err != nil {
			return
		}
		var ok bool
		if pkt, ok = self.prober.TagToPacket(tag, int32(self.timestamp)); ok {
			if pkt.DataType == int8(flvio.TAG_VIDEO) && pkt.AVCPacketType == uint8(flvio.AVC_SEQHDR) ||
				pkt.DataType == int8(flvio.TAG_AUDIO) && pkt.AVCPacketType == uint8(flvio.AAC_SEQHDR) {
				// 读到seq header,检查如果内容有变化，则更新header信息
				pkt.HeaderChanged, err = self.headerChanged(tag)
				if err != nil {
					err = fmt.Errorf("fails to resolve seq header: %s", err.Error())
					return
				}
				if pkt.HeaderChanged {
					log.Info().Str("id", self.prober.TaskID).Int8("tagtype", pkt.DataType).Msg("seq header is changed")
					return
				}
				// seq header内容不变,忽略
				log.Info().Str("id", self.prober.TaskID).Int8("tagtype", pkt.DataType).Msg("recv same seq header, ignore")
				continue
			} else if pkt.DataType == int8(flvio.TAG_SCRIPTDATA) {
				pkt.HeaderChanged = true //此类型tag只用来触发调用者重新发送header
			} else if pkt.IsKeyFrame {
				// 解析关键帧 看是否包含SPS信息
				self.digKeyFrame(pkt.Data)
			}
			return
		}
	}
}

func (self *conn) headerChanged(tag flvio.Tag) (updated bool, err error) {
	updated, err = self.prober.HeaderChanged(tag)
	if updated {
		self.streams = self.prober.Streams
	}
	return
}

func (self *conn) digKeyFrame(data []byte) {
	self.prober.DigKeyFrame(data)
}

func (self *conn) SetProberTaskID(taskID string) {
	self.prober.TaskID = taskID
}

func (self *conn) Prepare() (err error) {
	return self.prepare(stageCommandDone, 0)
}

func (self *conn) prepare(stage int, flags int) (err error) {
	for self.stage < stage {
		switch self.stage {
		case 0:
			if self.opts.IsServer {
				if err = self.HandshakeServer(); err != nil {
					return
				}
			} else {
				if err = self.HandshakeClient(); err != nil {
					return
				}
			}

		case stageHandshakeDone:
			if self.opts.IsServer {
				if err = self.ReadConnect(); err != nil {
					return
				}
			} else {
				if flags == prepareReading {
					if err = self.connectPlay(); err != nil {
						return
					}
				} else {
					if err = self.connectPublish(); err != nil {
						return
					}
				}
			}

		case stageCommandDone:
			if flags == prepareReading {
				if err = self.probe(); err != nil {
					return
				}
			} else {
				err = fmt.Errorf("rtmp: call WriteHeader() before WritePacket()")
				return
			}
		}
	}
	return
}

func (self *conn) Headers() (streams []av.CodecData, err error) {
	if err = self.prepare(stageCodecDataDone, prepareReading); err != nil {
		return
	}
	streams = self.streams
	for _, header := range streams {
		tag, ok, err := flv.CodecDataToTag(header)
		if err != nil {
			log.Error().Err(err).Str("ID", self.Info().ID).Str("domain", self.Info().Domain).Msg("[rtmp] invalid header")
		} else {
			log.Info().Str("ID", self.Info().ID).Str("domain", self.Info().Domain).Bool("ok", ok).Any("header", tag).Any("header.data", hex.EncodeToString(tag.Data)).Msg("[rtmp] Headers")
		}
	}
	return
}

func (self *conn) WritePacket(pkt av.Packet) (err error) {
	if err = self.prepare(stageCodecDataDone, prepareWriting); err != nil {
		return
	}
	if pkt.Idx < 0 || pkt.Idx >= int8(len(self.streams)) {
		err = errors.New("invalid packet idx " + strconv.Itoa(int(pkt.Idx)) + ", codecdata size " + strconv.Itoa(len(self.streams)))
		return
	}

	var tag flvio.Tag
	var timestamp int32
	if self.opts.VideoHeaderCheck {
		stream := self.streams[pkt.Idx]
		if pkt.DataType == int8(flvio.TAG_VIDEO) && !stream.Type().IsVideo() {
			err = errors.New("video packet type not match codecdata type")
			return
		}
		if pkt.DataType == int8(flvio.TAG_AUDIO) && !stream.Type().IsAudio() {
			err = errors.New("audio packet type not match codecdata type")
			return
		}
		tag, timestamp = flv.PacketToTag(pkt, stream)
	} else {
		stream := self.streams[pkt.Idx]
		if pkt.DataType == int8(flvio.TAG_AUDIO) && !stream.Type().IsAudio() {
			err = errors.New("audio packet type not match codecdata type")
			return
		}
		tag, timestamp = flv.PacketToTag(pkt, stream)
	}

	if Debug {
		log.Debug().Any("packet", pkt).Msg("[rtmp] WritePacket")
	}

	if err = self.writeAVTag(tag, timestamp); err != nil {
		return
	}
	return
}

func (self *conn) WriteTrailer() (err error) {
	if err = self.flushWrite(); err != nil {
		return
	}
	return
}

func (self *conn) WriteHeader(streams []av.CodecData) (err error) {
	if err = self.prepare(stageCommandDone, prepareWriting); err != nil {
		return
	}

	if len(streams) == 0 {
		return
	}

	var metadata flvio.AMFMap
	if metadata, err = flv.NewMetadataByStreams(streams); err != nil {
		return
	}

	// > onMetaData()
	if err = self.writeDataMsg(5, self.avmsgsid, "onMetaData", metadata); err != nil {
		return
	}

	log.Info().Str("ID", self.Info().ID).Str("domain", self.Info().Domain).Any("onMetadata", metadata).Msg("[rtmp] WriteHeader")

	// > Videodata(decoder config)
	// > Audiodata(decoder config)
	for _, stream := range streams {
		var ok bool
		var tag flvio.Tag
		if tag, ok, err = flv.CodecDataToTag(stream); err != nil {
			return
		}
		if ok {
			if err = self.writeAVTag(tag, 0); err != nil {
				return
			}
		}
		log.Debug().Str("ID", self.Info().ID).Str("domain", self.Info().Domain).Any("header", tag).Str("tag.data", hex.EncodeToString(tag.Data)).Msg("[rtmp] WriteHeader")
	}

	log.Debug().Str("ID", self.Info().ID).Str("domain", self.Info().Domain).Msg("[rtmp] WriteHeader end")

	self.streams = streams
	self.stage++
	return
}

func (self *conn) tmpwbuf(n int) []byte {
	if len(self.writebuf) < n {
		self.writebuf = make([]byte, n)
	}
	return self.writebuf
}

func (self *conn) writeSetChunkSize(size int) (err error) {
	self.writeMaxChunkSize = size
	b := self.tmpwbuf(chunkHeaderLength + 4)
	n := self.fillChunkHeader(b, 2, 0, msgtypeidSetChunkSize, 0, 4)
	pio.PutU32BE(b[n:], uint32(size))
	n += 4
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		err = fmt.Errorf("writeSetChunkSize: %s", err.Error())
		self.debug("send SetChunkSize error headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 chunksize=%d %s", msgtypeidSetChunkSize, size, err.Error())
		return
	}
	self.debug("send SetChunkSize headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 chunksize=%d", msgtypeidSetChunkSize, size)
	return
}

func (self *conn) writeAck(seqnum uint32) (err error) {
	b := self.tmpwbuf(chunkHeaderLength + 4)
	n := self.fillChunkHeader(b, 2, 0, msgtypeidAck, 0, 4)
	pio.PutU32BE(b[n:], seqnum)
	n += 4
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		err = fmt.Errorf("writeAck: %s", err.Error())
		self.debug("send ack error headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 seqnum=%d %s", msgtypeidAck, seqnum, err.Error())
		return
	}
	self.debug("send ack headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 seqnum=%d", msgtypeidAck, seqnum)

	return
}

func (self *conn) writeWindowAckSize(size uint32) (err error) {
	b := self.tmpwbuf(chunkHeaderLength + 4)
	n := self.fillChunkHeader(b, 2, 0, msgtypeidWindowAckSize, 0, 4)
	pio.PutU32BE(b[n:], size)
	n += 4
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		err = fmt.Errorf("writeWindowAckSize: %s", err.Error())
		self.debug("send WindowAckSize error headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 acksize=%d %s", msgtypeidWindowAckSize, size, err.Error())
		return
	}
	self.debug("send WindowAckSize headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 acksize=%d", msgtypeidWindowAckSize, size)
	return
}

func (self *conn) writeSetPeerBandwidth(acksize uint32, limittype uint8) (err error) {
	b := self.tmpwbuf(chunkHeaderLength + 5)
	n := self.fillChunkHeader(b, 2, 0, msgtypeidSetPeerBandwidth, 0, 5)
	pio.PutU32BE(b[n:], acksize)
	n += 4
	b[n] = limittype
	n++
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		err = fmt.Errorf("writeSetPeerBandwidth: %s", err.Error())
		self.debug("send SetPeerBandwidth error headertype=0 csid=2 ts=0 msglen=5 typeid=%d sid=0 acksize=%d limittype=%d %s", msgtypeidSetPeerBandwidth, acksize, limittype, err.Error())
		return
	}
	self.debug("send SetPeerBandwidth headertype=0 csid=2 ts=0 msglen=5 typeid=%d sid=0 acksize=%d limittype=%d", msgtypeidSetPeerBandwidth, acksize, limittype)
	return
}

func (self *conn) writeCommandMsg(csid, msgsid uint32, args ...interface{}) (err error) {
	err = self.writeAMF0Msg(msgtypeidCommandMsgAMF0, csid, msgsid, args...)
	if err != nil {
		err = fmt.Errorf("writeCommandMsg: csid=%d msgsid=%d args=%+v err=%s ", csid, msgsid, args, err.Error())
	}
	return
}

func (self *conn) writeDataMsg(csid, msgsid uint32, args ...interface{}) (err error) {
	err = self.writeAMF0Msg(msgtypeidDataMsgAMF0, csid, msgsid, args...)
	if err != nil {
		err = fmt.Errorf("writeDataMsg: csid=%d msgsid=%d args=%+v err=%s ", csid, msgsid, args, err.Error())
	}
	return
}

func (self *conn) writeAMF0Msg(msgtypeid uint8, csid, msgsid uint32, args ...interface{}) (err error) {
	size := 0
	for _, arg := range args {
		size += flvio.LenAMF0Val(arg)
	}

	b := self.tmpwbuf(chunkHeaderLength + size)
	n := self.fillChunkHeader(b, csid, 0, msgtypeid, msgsid, size)
	for _, arg := range args {
		n += flvio.FillAMF0Val(b[n:], arg)
	}

	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		self.debug("send AMF0Msg error headertype=0 csid=%d ts=0 msglen=%d msgtypeid=%d msgsid=%d msg=%+v %s", csid, size, msgtypeid, msgsid, args, err.Error())
		return
	}
	self.debug("send AMF0Msg headertype=0 csid=%d ts=0 msglen=%d msgtypeid=%d msgsid=%d msg=%+v", csid, size, msgtypeid, msgsid, args)
	return
}

func (self *conn) writeAVTag(tag flvio.Tag, ts int32) (err error) {
	var msgtypeid uint8
	var csid uint32
	var data []byte

	switch tag.Type {
	case flvio.TAG_AUDIO:
		msgtypeid = msgtypeidAudioMsg
		csid = 6
		data = tag.Data

	case flvio.TAG_VIDEO:
		msgtypeid = msgtypeidVideoMsg
		csid = 7
		data = tag.Data
	}

	actualChunkHeaderLength := chunkHeaderLength
	if uint32(ts) > FlvTimestampMax {
		actualChunkHeaderLength += 4
	}

	if ts < 0 {
		log.Info().Str("ID", self.Info().ID).Int32("ts", ts).Msg("[rtmp] writeAVTag negative ts")
	}

	b := self.tmpwbuf(actualChunkHeaderLength + flvio.MaxTagSubHeaderLength)
	hdrlen := tag.FillHeader(b[actualChunkHeaderLength:])
	self.fillChunkHeader(b, csid, ts, msgtypeid, self.avmsgsid, hdrlen+len(data))
	n := hdrlen + actualChunkHeaderLength

	if n+len(data) > self.writeMaxChunkSize {
		if err = self.writeSetChunkSize(n + len(data)); err != nil {
			return
		}
	}

	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = self.bufw.Write(b[:n]); err != nil {
		err = fmt.Errorf("writeAVTag write header: %s", err.Error())
		self.debug("send avtag error headertype=0 csid=%d ts=%d msglen=%d msgtypeid=%d msgsid=%d chunkheaderlen=%d tagheaderlen=%d datalen=%d tagtype=%d tagframetype=%d avcpackettype=%d aacpackettype=%d %s",
			csid, ts, hdrlen+len(data), msgtypeid, self.avmsgsid, actualChunkHeaderLength, hdrlen, len(data), tag.Type, tag.FrameType, tag.AVCPacketType, tag.AACPacketType, err.Error())
		return
	}
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(data)
	if err != nil {
		err = fmt.Errorf("writeAVTag write data: %s", err.Error())
		self.debug("send avtag error headertype=0 csid=%d ts=%d msglen=%d msgtypeid=%d msgsid=%d chunkheaderlen=%d tagheaderlen=%d datalen=%d tagtype=%d tagframetype=%d avcpackettype=%d aacpackettype=%d %s",
			csid, ts, hdrlen+len(data), msgtypeid, self.avmsgsid, actualChunkHeaderLength, hdrlen, len(data), tag.Type, tag.FrameType, tag.AVCPacketType, tag.AACPacketType, err.Error())
		return
	}
	self.debug("send avtag headertype=0 csid=%d ts=%d msglen=%d msgtypeid=%d msgsid=%d chunkheaderlen=%d tagheaderlen=%d datalen=%d tagtype=%d tagframetype=%d avcpackettype=%d aacpackettype=%d",
		csid, ts, hdrlen+len(data), msgtypeid, self.avmsgsid, actualChunkHeaderLength, hdrlen, len(data), tag.Type, tag.FrameType, tag.AVCPacketType, tag.AACPacketType)
	return
}

func (self *conn) writeStreamBegin(msgsid uint32) (err error) {
	b := self.tmpwbuf(chunkHeaderLength + 6)
	n := self.fillChunkHeader(b, 2, 0, msgtypeidUserControl, 0, 6)
	pio.PutU16BE(b[n:], eventtypeStreamBegin)
	n += 2
	pio.PutU32BE(b[n:], msgsid)
	n += 4
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		err = fmt.Errorf("writeStreamBegin: %s", err.Error())
		self.debug("send streambegin error headertype=0 csid=2 ts=0 msglen=6 msgtypeid=%d msgsid=0 msgsidinbody=%d %s", msgtypeidUserControl, msgsid, err.Error())
		return
	}
	self.debug("send streambegin headertype=0 csid=2 ts=0 msglen=4 msgtypeid=%d msgsid=0 msgsidinbody=%d", msgtypeidUserControl, msgsid)
	return
}

func (self *conn) writeSetBufferLength(msgsid uint32, timestamp uint32) (err error) {
	b := self.tmpwbuf(chunkHeaderLength + 10)
	n := self.fillChunkHeader(b, 2, 0, msgtypeidUserControl, 0, 10)
	pio.PutU16BE(b[n:], eventtypeSetBufferLength)
	n += 2
	pio.PutU32BE(b[n:], msgsid)
	n += 4
	pio.PutU32BE(b[n:], timestamp)
	n += 4
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	_, err = self.bufw.Write(b[:n])
	if err != nil {
		err = fmt.Errorf("writeSetBufferLength: %s", err.Error())
		self.debug("send setbufferlength error headertype=0 csid=2 ts=0 msglen=10 msgtypeid=%d msgsid=0 msgsidinbody=%d timestamp=%d %s", msgtypeidUserControl, msgsid, timestamp, err.Error())
		return
	}
	self.debug("send setbufferlength error headertype=0 csid=2 ts=0 msglen=10 msgtypeid=%d msgsid=0 msgsidinbody=%d timestamp=%d", msgtypeidUserControl, msgsid, timestamp)
	return
}

const chunkHeaderLength = 12
const FlvTimestampMax = 0xFFFFFF

func (self *conn) fillChunkHeader(b []byte, csid uint32, timestamp int32, msgtypeid uint8, msgsid uint32, msgdatalen int) (n int) {
	//  0                   1                   2                   3
	//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
	// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	// |                   timestamp                   |message length |
	// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	// |     message length (cont)     |message type id| msg stream id |
	// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	// |           message stream id (cont)            |
	// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
	//
	//       Figure 9 Chunk Message Header – Type 0

	b[n] = byte(csid) & 0x3f
	n++
	if uint32(timestamp) <= FlvTimestampMax {
		pio.PutU24BE(b[n:], uint32(timestamp))
	} else {
		pio.PutU24BE(b[n:], FlvTimestampMax)
	}
	n += 3
	pio.PutU24BE(b[n:], uint32(msgdatalen))
	n += 3
	b[n] = msgtypeid
	n++
	pio.PutU32LE(b[n:], msgsid)
	n += 4
	if uint32(timestamp) > FlvTimestampMax {
		pio.PutU32BE(b[n:], uint32(timestamp))
		n += 4
	}

	if Debug {
		log.Debug().Int("msgdatalen", msgdatalen).Uint32("msgid", msgsid).Msg("[rtmp] write chunk")
	}

	return
}

func (self *conn) flushWrite() (err error) {
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if err = self.bufw.Flush(); err != nil {
		err = fmt.Errorf("rtmp: flushWrite: %s", err.Error())
		return
	}
	return
}

func (self *conn) readChunk() (err error) {
	b := self.readbuf
	n := 0
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = io.ReadFull(self.bufr, b[:1]); err != nil {
		err = fmt.Errorf("read rtmp chunk header first byte: %s", err.Error())
		self.debug("recv error %s", err.Error())
		return
	}
	header := b[0]
	n += 1

	var msghdrtype uint8
	var csid uint32

	msghdrtype = header >> 6

	csid = uint32(header) & 0x3f
	switch csid {
	default: // Chunk basic header 1
	case 0: // Chunk basic header 2
		self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
		if _, err = io.ReadFull(self.bufr, b[:1]); err != nil {
			err = fmt.Errorf("read rtmp chunk header headertype=%d csid=0: %s", msghdrtype, err.Error())
			self.debug("recv error %s", err.Error())
			return
		}
		n += 1
		csid = uint32(b[0]) + 64
	case 1: // Chunk basic header 3
		self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
		if _, err = io.ReadFull(self.bufr, b[:2]); err != nil {
			err = fmt.Errorf("read rtmp chunk header headertype=%d csid=1: %s", msghdrtype, err.Error())
			self.debug("recv err %s", err.Error())
			return
		}
		n += 2
		csid = uint32(pio.U16BE(b)) + 64
	}

	cs := self.readcsmap[csid]
	if cs == nil {
		cs = &chunkStream{}
		self.readcsmap[csid] = cs
	}

	var timestamp uint32

	switch msghdrtype {
	case 0:
		//  0                   1                   2                   3
		//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		// |                   timestamp                   |message length |
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		// |     message length (cont)     |message type id| msg stream id |
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		// |           message stream id (cont)            |
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		//
		//       Figure 9 Chunk Message Header – Type 0
		if cs.msgdataleft != 0 {
			err = fmt.Errorf("headertype=%d csid=%d msgdataleft=%d chunk invalid", msghdrtype, csid, cs.msgdataleft)
			self.debug("recv error %s", err.Error())
			return
		}
		h := b[:11]
		self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
		if _, err = io.ReadFull(self.bufr, h); err != nil {
			err = fmt.Errorf("headertype=%d csid=%d read header %s", msghdrtype, csid, err.Error())
			self.debug("recv error %s", err.Error())
			return
		}
		n += len(h)
		timestamp = pio.U24BE(h[0:3])
		cs.msghdrtype = msghdrtype
		cs.msgdatalen = pio.U24BE(h[3:6])
		cs.msgtypeid = h[6]
		cs.msgsid = pio.U32LE(h[7:11])
		if timestamp == 0xffffff {
			self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
			if _, err = io.ReadFull(self.bufr, b[:4]); err != nil {
				err = fmt.Errorf("headertype=%d csid=%d read ext timestamp: %s", msghdrtype, csid, err.Error())
				self.debug("recv error %s", err.Error())
				return
			}
			n += 4
			timestamp = pio.U32BE(b)
			cs.hastimeext = true
		} else {
			cs.hastimeext = false
		}
		cs.timenow = timestamp
		cs.Start()

	case 1:
		//  0                   1                   2                   3
		//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		// |                timestamp delta                |message length |
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		// |     message length (cont)     |message type id|
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		//
		//       Figure 10 Chunk Message Header – Type 1
		if cs.msgdataleft != 0 {
			err = fmt.Errorf("headertype=%d csid=%d msgdataleft=%d chunk invalid", msghdrtype, csid, cs.msgdataleft)
			self.debug("recv error %s", err.Error())
			return
		}
		h := b[:7]
		self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
		if _, err = io.ReadFull(self.bufr, h); err != nil {
			err = fmt.Errorf("headertype=%d csid=%d read header %s", msghdrtype, csid, err.Error())
			self.debug("recv error %s", err.Error())
			return
		}
		n += len(h)
		timestamp = pio.U24BE(h[0:3])
		cs.msghdrtype = msghdrtype
		cs.msgdatalen = pio.U24BE(h[3:6])
		cs.msgtypeid = h[6]
		if timestamp == 0xffffff {
			self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
			if _, err = io.ReadFull(self.bufr, b[:4]); err != nil {
				err = fmt.Errorf("headertype=%d csid=%d read ext timestamp: %s", msghdrtype, csid, err.Error())
				self.debug("recv error %s", err.Error())
				return
			}
			n += 4
			timestamp = pio.U32BE(b)
			cs.hastimeext = true
		} else {
			cs.hastimeext = false
		}
		cs.timedelta = timestamp
		cs.timenow += timestamp
		cs.Start()

	case 2:
		//  0                   1                   2
		//  0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		// |                timestamp delta                |
		// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
		//
		//       Figure 11 Chunk Message Header – Type 2
		if cs.msgdataleft != 0 {
			err = fmt.Errorf("headertype=%d csid=%d msgdataleft=%d chunk invalid", msghdrtype, csid, cs.msgdataleft)
			self.debug("recv error %s", err.Error())
			return
		}
		h := b[:3]
		self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
		if _, err = io.ReadFull(self.bufr, h); err != nil {
			err = fmt.Errorf("headertype=%d csid=%d read header %s", msghdrtype, csid, err.Error())
			self.debug("recv error %s", err.Error())
			return
		}
		n += len(h)
		cs.msghdrtype = msghdrtype
		timestamp = pio.U24BE(h[0:3])
		if timestamp == 0xffffff {
			self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
			if _, err = io.ReadFull(self.bufr, b[:4]); err != nil {
				err = fmt.Errorf("headertype=%d csid=%d read ext timestamp: %s", msghdrtype, csid, err.Error())
				self.debug("recv error %s", err.Error())
				return
			}
			n += 4
			timestamp = pio.U32BE(b)
			cs.hastimeext = true
		} else {
			cs.hastimeext = false
		}
		cs.timedelta = timestamp
		cs.timenow += timestamp
		cs.Start()

	case 3:
		if cs.msgdataleft == 0 {
			switch cs.msghdrtype {
			case 0:
				if cs.hastimeext {
					self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
					if _, err = io.ReadFull(self.bufr, b[:4]); err != nil {
						err = fmt.Errorf("headertype=%d csid=%d->%d read ext timestamp: %s", msghdrtype, cs.msghdrtype, csid, err.Error())
						self.debug("recv error %s", err.Error())
						return
					}
					n += 4
					timestamp = pio.U32BE(b)
					cs.timenow = timestamp
				}
			case 1, 2:
				if cs.hastimeext {
					self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
					if _, err = io.ReadFull(self.bufr, b[:4]); err != nil {
						err = fmt.Errorf("headertype=%d csid=%d->%d read ext timestamp: %s", msghdrtype, cs.msghdrtype, csid, err.Error())
						self.debug("recv error %s", err.Error())
						return
					}
					n += 4
					timestamp = pio.U32BE(b)
				} else {
					timestamp = cs.timedelta
				}
				cs.timenow += timestamp
			}
			cs.Start()
		} else {
			if cs.hastimeext {
				var tbs []byte
				tbs, err = self.bufr.Peek(4)
				if err != nil {
					err = fmt.Errorf("headertype=%d csid=%d->%d try peek timestamp: %s", msghdrtype, cs.msghdrtype, csid, err.Error())
					self.debug("recv error %s", err.Error())
					return
				}
				tmpts := pio.U32BE(tbs)
				if tmpts > 0 && tmpts == cs.timenow {
					self.bufr.Discard(4)
					log.Debug().Uint32("csid", csid).Uint32("timestamp", tmpts).Str("taskid", self.prober.TaskID).Msg("[rtmp] discard ext timestamp")
				}
			}
		}

	default:
		err = fmt.Errorf("headertype=%d csid=%d invalid headertype", msghdrtype, csid)
		self.debug("recv error %s", err.Error())
		return
	}

	size := int(cs.msgdataleft)
	if size > self.readMaxChunkSize {
		size = self.readMaxChunkSize
	}
	off := cs.msgdatalen - cs.msgdataleft
	buf := cs.msgdata[off : int(off)+size]
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	start := time.Now()
	if _, err = io.ReadFull(self.bufr, buf); err != nil {
		err = fmt.Errorf("read lefted rtmp data: %s size=%d offset=%d cost=%d rwtimeout=%v",
			err.Error(), size, off, time.Since(start).Nanoseconds()/1e6, self.opts.ReadWriteTimeout)
		self.debug("recv error headertype=%d csid=%d ts=%d msglen=%d msgtypeid=%d msgsid=%d chunksize=%d offset=%d timenow=%d timedelta=%d %s",
			msghdrtype, csid, timestamp, cs.msgdatalen, cs.msgtypeid, cs.msgsid, size, off, cs.timenow, cs.timedelta, err.Error())
		return
	}
	n += len(buf)
	cs.msgdataleft -= uint32(size)

	self.debug("recv chunk headertype=%d csid=%d ts=%d msglen=%d msgtypeid=%d msgsid=%d chunksize=%d offset=%d timenow=%d timedelta=%d",
		msghdrtype, csid, timestamp, cs.msgdatalen, cs.msgtypeid, cs.msgsid, size, off, cs.timenow, cs.timedelta)

	if cs.msgdataleft == 0 {

		if err = self.handleMsg(cs.timenow, cs.msgsid, cs.msgtypeid, cs.msgdata); err != nil {
			return
		}
	}

	self.ackn += uint32(n)
	if self.readAckSize != 0 && self.ackn > self.readAckSize {
		if err = self.writeAck(self.ackn); err != nil {
			err = fmt.Errorf("write ack: %s ack=%d", err.Error(), self.ackn)
			return
		}
		self.ackn = 0
	}

	return
}

func (self *conn) handleCommandMsgAMF0(b []byte) (n int, err error) {
	var name, transid, obj interface{}
	var size int

	if name, size, err = flvio.ParseAMF0Val(b[n:]); err != nil {
		err = fmt.Errorf("handleCommandMsgAMF0: get name: %s", err.Error())
		return
	}
	n += size
	if transid, size, err = flvio.ParseAMF0Val(b[n:]); err != nil {
		err = fmt.Errorf("handleCommandMsgAMF0: get transid: %s", err.Error())
		return
	}
	n += size
	if obj, size, err = flvio.ParseAMF0Val(b[n:]); err != nil {
		err = fmt.Errorf("handleCommandMsgAMF0: get obj: %s", err.Error())
		return
	}
	n += size

	var ok bool
	if self.commandname, ok = name.(string); !ok {
		err = fmt.Errorf("rtmp: CommandMsgAMF0 command is not string")
		return
	}
	self.commandtransid, _ = transid.(float64)
	self.commandobj, _ = obj.(flvio.AMFMap)
	self.commandparams = []interface{}{}

	for n < len(b) {
		if obj, size, err = flvio.ParseAMF0Val(b[n:]); err != nil {
			err = fmt.Errorf("handleCommandMsgAMF0: get commandparams: %s", err.Error())
			return
		}
		n += size
		self.commandparams = append(self.commandparams, obj)
	}
	if n < len(b) {
		err = fmt.Errorf("rtmp: CommandMsgAMF0 left bytes=%d", len(b)-n)
		return
	}

	self.gotcommand = true
	return
}

func (self *conn) handleMsg(timestamp uint32, msgsid uint32, msgtypeid uint8, msgdata []byte) (err error) {
	self.msgdata = msgdata
	self.msgtypeid = msgtypeid
	self.timestamp = timestamp

	switch msgtypeid {
	case msgtypeidCommandMsgAMF0:
		if _, err = self.handleCommandMsgAMF0(msgdata); err != nil {
			return
		}

	case msgtypeidCommandMsgAMF3:
		if len(msgdata) < 1 {
			err = fmt.Errorf("rtmp: short packet of CommandMsgAMF3")
			return
		}
		// skip first byte
		if _, err = self.handleCommandMsgAMF0(msgdata[1:]); err != nil {
			return
		}

	case msgtypeidUserControl:
		if len(msgdata) < 2 {
			err = fmt.Errorf("rtmp: short packet of UserControl")
			return
		}
		self.eventtype = pio.U16BE(msgdata)
		log.Debug().Str("taskid", self.prober.TaskID).Str("role", self.opts.RoleID).Uint16("eventtype", self.eventtype).Msg("handleMsg: unhandled msg: msgtypeidUserControl")

	case msgtypeidDataMsgAMF0:
		b := msgdata
		n := 0
		for n < len(b) {
			var obj interface{}
			var size int
			if obj, size, err = flvio.ParseAMF0Val(b[n:]); err != nil {
				err = fmt.Errorf("handleMsg: msgtypeidDataMsgAMF0: %s", err.Error())
				return
			}
			n += size
			self.datamsgvals = append(self.datamsgvals, obj)
		}
		if n < len(b) {
			err = fmt.Errorf("rtmp: DataMsgAMF0 left bytes=%d", len(b)-n)
			return
		}
		tag := flvio.Tag{Type: flvio.TAG_SCRIPTDATA}
		self.scripttag = tag

	case msgtypeidVideoMsg:
		if len(msgdata) == 0 {
			return
		}
		tag := flvio.Tag{Type: flvio.TAG_VIDEO}
		var n int
		if n, err = (&tag).ParseHeader(msgdata); err != nil {
			return
		}
		if !(tag.FrameType == flvio.FRAME_INTER || tag.FrameType == flvio.FRAME_KEY) {
			return
		}
		tag.Data = msgdata[n:]
		self.avtag = tag

	case msgtypeidAudioMsg:
		if len(msgdata) == 0 {
			return
		}
		tag := flvio.Tag{Type: flvio.TAG_AUDIO}
		var n int
		if n, err = (&tag).ParseHeader(msgdata); err != nil {
			return
		}
		tag.Data = msgdata[n:]
		self.avtag = tag

	case msgtypeidSetChunkSize:
		if len(msgdata) < 4 {
			err = fmt.Errorf("rtmp: short packet of SetChunkSize")
			return
		}
		self.readMaxChunkSize = int(pio.U32BE(msgdata))
		log.Info().Uint8("msgtypeid", msgtypeid).Uint32("msgid", msgsid).Uint32("timestamp", timestamp).Str("taskid", self.prober.TaskID).Int("chunksize", self.readMaxChunkSize).Msg("[rtmp] command SetChunkSize")
		return
	default:
		log.Debug().Uint8("msgtypeid", msgtypeid).Uint32("msgsid", msgsid).Uint32("timestamp", timestamp).Str("taskid", self.prober.TaskID).Str("role", self.opts.RoleID).Msg("handleMsg: unhandled msg")
	}

	self.gotmsg = true
	return
}

var (
	hsClientFullKey = []byte{
		'G', 'e', 'n', 'u', 'i', 'n', 'e', ' ', 'A', 'd', 'o', 'b', 'e', ' ',
		'F', 'l', 'a', 's', 'h', ' ', 'P', 'l', 'a', 'y', 'e', 'r', ' ',
		'0', '0', '1',
		0xF0, 0xEE, 0xC2, 0x4A, 0x80, 0x68, 0xBE, 0xE8, 0x2E, 0x00, 0xD0, 0xD1,
		0x02, 0x9E, 0x7E, 0x57, 0x6E, 0xEC, 0x5D, 0x2D, 0x29, 0x80, 0x6F, 0xAB,
		0x93, 0xB8, 0xE6, 0x36, 0xCF, 0xEB, 0x31, 0xAE,
	}
	hsServerFullKey = []byte{
		'G', 'e', 'n', 'u', 'i', 'n', 'e', ' ', 'A', 'd', 'o', 'b', 'e', ' ',
		'F', 'l', 'a', 's', 'h', ' ', 'M', 'e', 'd', 'i', 'a', ' ',
		'S', 'e', 'r', 'v', 'e', 'r', ' ',
		'0', '0', '1',
		0xF0, 0xEE, 0xC2, 0x4A, 0x80, 0x68, 0xBE, 0xE8, 0x2E, 0x00, 0xD0, 0xD1,
		0x02, 0x9E, 0x7E, 0x57, 0x6E, 0xEC, 0x5D, 0x2D, 0x29, 0x80, 0x6F, 0xAB,
		0x93, 0xB8, 0xE6, 0x36, 0xCF, 0xEB, 0x31, 0xAE,
	}
	hsClientPartialKey = hsClientFullKey[:30]
	hsServerPartialKey = hsServerFullKey[:36]
)

func hsMakeDigest(key []byte, src []byte, gap int) (dst []byte) {
	h := hmac.New(sha256.New, key)
	if gap <= 0 {
		h.Write(src)
	} else {
		h.Write(src[:gap])
		h.Write(src[gap+32:])
	}
	return h.Sum(nil)
}

func hsCalcDigestPos(p []byte, base int) (pos int) {
	for i := 0; i < 4; i++ {
		pos += int(p[base+i])
	}
	pos = (pos % 728) + base + 4
	return
}

func hsFindDigest(p []byte, key []byte, base int) int {
	gap := hsCalcDigestPos(p, base)
	digest := hsMakeDigest(key, p, gap)
	if bytes.Compare(p[gap:gap+32], digest) != 0 {
		return -1
	}
	return gap
}

func hsParse1(p []byte, peerkey []byte, key []byte) (ok bool, digest []byte) {
	var pos int
	if pos = hsFindDigest(p, peerkey, 772); pos == -1 {
		if pos = hsFindDigest(p, peerkey, 8); pos == -1 {
			return
		}
	}
	ok = true
	digest = hsMakeDigest(key, p[pos:pos+32], -1)
	return
}

func hsCreate01(p []byte, time uint32, ver uint32, key []byte) {
	p[0] = 3
	p1 := p[1:]
	rand.Read(p1[8:])
	pio.PutU32BE(p1[0:4], time)
	pio.PutU32BE(p1[4:8], ver)
	gap := hsCalcDigestPos(p1, 8)
	digest := hsMakeDigest(key, p1, gap)
	copy(p1[gap:], digest)
}

func hsCreate2(p []byte, key []byte) {
	rand.Read(p)
	gap := len(p) - 32
	digest := hsMakeDigest(key, p, gap)
	copy(p[gap:], digest)
}

func (self *conn) HandshakeClient() error {
	var err error
	var random [(1 + 1536*2) * 2]byte

	C0C1C2 := random[:1536*2+1]
	C0 := C0C1C2[:1]
	//C1 := C0C1C2[1:1536+1]
	C0C1 := C0C1C2[:1536+1]
	C2 := C0C1C2[1536+1:]

	S0S1S2 := random[1536*2+1:]
	//S0 := S0S1S2[:1]
	S1 := S0S1S2[1 : 1536+1]
	//S0S1 := S0S1S2[:1536+1]
	//S2 := S0S1S2[1536+1:]

	C0[0] = 3
	//hsCreate01(C0C1, hsClientFullKey)

	self.debug("localaddr=%s remoteaddr=%s", self.netconn.LocalAddr().String(), self.netconn.RemoteAddr().String())
	// > C0C1
	self.debug("send handshake C0C1")
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = self.bufw.Write(C0C1); err != nil {
		return errors.Wrap(err, "rtmp HandshakeClient")
	}
	if err = self.bufw.Flush(); err != nil {
		return errors.Wrap(err, "rtmp HandshakeClient")
	}

	// < S0S1S2
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = io.ReadFull(self.bufr, S0S1S2); err != nil {
		return errors.Wrap(err, "rtmp HandshakeClient")
	}
	self.debug("recv handshake S0S1S2 server version " + fmt.Sprint(S1[4], S1[5], S1[6], S1[7]))

	if ver := pio.U32BE(S1[4:8]); ver != 0 {
		C2 = S1
	} else {
		C2 = S1
	}

	// > C2
	self.debug("send handshake C2")
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = self.bufw.Write(C2); err != nil {
		return errors.Wrap(err, "rtmp HandshakeClient")
	}

	self.stage++
	return nil
}

func (self *conn) HandshakeServer() (err error) {
	var random [(1 + 1536*2) * 2]byte

	C0C1C2 := random[:1536*2+1]
	C0 := C0C1C2[:1]
	C1 := C0C1C2[1 : 1536+1]
	C0C1 := C0C1C2[:1536+1]
	C2 := C0C1C2[1536+1:]

	S0S1S2 := random[1536*2+1:]
	S0 := S0S1S2[:1]
	S1 := S0S1S2[1 : 1536+1]
	S0S1 := S0S1S2[:1536+1]
	S2 := S0S1S2[1536+1:]

	// < C0C1
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = io.ReadFull(self.bufr, C0C1); err != nil {
		return
	}
	if C0[0] != 3 {
		err = fmt.Errorf("rtmp: handshake version=%d invalid", C0[0])
		return
	}

	S0[0] = 3

	clitime := pio.U32BE(C1[0:4])
	srvtime := clitime
	srvver := uint32(0x0d0e0a0d)
	cliver := pio.U32BE(C1[4:8])

	if cliver != 0 {
		var ok bool
		var digest []byte
		if ok, digest = hsParse1(C1, hsClientPartialKey, hsServerFullKey); !ok {
			err = fmt.Errorf("rtmp: handshake server: C1 invalid")
			return
		}
		hsCreate01(S0S1, srvtime, srvver, hsServerPartialKey)
		hsCreate2(S2, digest)
	} else {
		copy(S1, C1)
		copy(S2, C2)
	}

	// > S0S1S2
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = self.bufw.Write(S0S1S2); err != nil {
		return
	}
	if err = self.bufw.Flush(); err != nil {
		return
	}

	// < C2
	self.netconn.SetDeadline(time.Now().Add(self.opts.ReadWriteTimeout))
	if _, err = io.ReadFull(self.bufr, C2); err != nil {
		return
	}

	self.stage++
	return
}

// debug 写入debug信息
func (self *conn) debug(format string, args ...interface{}) {
	if !self.debuger.Enabled() {
		return
	}
	self.debuger.Debug(self.opts.RoleID+" "+format, args...)
}

func (self *conn) RemoteAddr() string {
	if self.netconn != nil {
		return self.netconn.RemoteAddr().String()
	}
	return ""
}

func (self *conn) Info() common.Info {
	if self == nil {
		return common.Info{}
	}
	self.info.IsPlaying = self.playing
	self.info.IsPublishing = self.publishing
	return self.info
}

func ParseURLDetail(uri string) (u *url.URL, host, app, streamID string, err error) {
	if u, err = url.Parse(uri); err != nil {
		return
	}
	ss := strings.Split(u.Path, "/")
	host = u.Host
	if len(ss) == 3 { // e.g "/app/stream"
		app = ss[1]
		streamID = ss[2]
	} else if len(ss) == 4 { // "/host/app/stream"
		host = ss[1]
		app = ss[2]
		streamID = ss[3]
	}
	speHost := u.Query().Get("speHost")
	if speHost != "" {
		host = speHost
	}
	host = utils.PeelOffPort1935(host)
	return
}

func (self *conn) ConnectPublish() error {
	return self.connectPublish()
}

func (self *conn) ConnectPlay() error {
	return self.connectPlay()
}

func (self *conn) WriteHeaders(headers []av.Header) error {
	return self.WriteHeader(avutil.RevertHeader(headers))
}

func (self *conn) ReadHeaders() ([]av.Header, error) {
	headers, err := self.Headers()
	if err != nil {
		return nil, err
	}
	return avutil.ConvertHeader(headers), nil
}

func (self *conn) VideoResolution() (width uint32, height uint32) {
	if self.streams == nil {
		return
	}
	for _, h := range self.streams {
		if h.Type() == av.H264 {
			data := h.(h264parser.CodecData)
			tag := data.SequnceHeaderTag.(flvio.Tag)
			videoHdr, _ := h264parser.NewCodecDataFromAVCDecoderConfRecord(tag.Data)
			width = uint32(videoHdr.SPSInfo.Width)
			height = uint32(videoHdr.SPSInfo.Height)
		}
	}
	return
}

func resolveStreamID(path string) string {
	ps := strings.Split(path, "?")
	if len(ps) > 0 {
		return ps[0]
	}
	return ""
}
