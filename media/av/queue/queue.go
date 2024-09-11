package queue

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"sync"
	"time"

	"github.com/bugVanisher/streamer/media/av"
	"github.com/bugVanisher/streamer/media/av/avutil"
	"github.com/bugVanisher/streamer/media/container/flv/flvio"

	util "github.com/bugVanisher/streamer/utils"
)

const (
	// DefaultGopCount 默认bufqueue缓存gop个数
	DefaultGopCount = 6
	// DefaultPktCount 默认bufqueue缓存pkt个数
	DefaultPktCount = 2000
	// DefaultTimeAdjustThreshold 默认时间修正阀值
	DefaultTimeAdjustThreshold = time.Second * 1
	// DefaultWaitTimeout
	DefaultWaitTimeout = time.Second * 10

	minPureAudioDuration = 10 * time.Second
)

//        time
// ----------------->
//
// V-A-V-V-A-V-V-A-V-V
// |                 |
// 0        5        10
// head             tail
// oldest          latest
//

// Header header info of stream
type Header struct {
	Datas   []av.Header
	BeginAt BufPos
}

// Stat ...
type Stat struct {
	PktCount     uint32 `json:"pkt_count"`
	LossPktCount uint32 `json:"loss_pkt_count"`
	GopCount     uint32 `json:"gop_count"`
	VideoCount   uint32 `json:"video_count"`
	AudioCount   uint32 `json:"audio_count"`
	HeadPos      int    `json:"head_pos"`
	TailPos      int    `json:"tail_pos"`
	Closed       bool   `json:"closed"`
}

//Queue buffer queue
type Queue struct {
	buf  *Buf
	lock *sync.RWMutex
	cond *sync.Cond

	headers  []Header
	videoidx int
	closed   bool

	maxGOPCount   int
	maxPktCount   int
	curGOPCount   int
	curVideoCount int
	curAudioCount int
	lossPktCount  uint32

	sid string
}

// NewQueue new a queue
func NewQueue() *Queue {
	q := &Queue{}
	q.buf = NewBuf()
	q.maxGOPCount = DefaultGopCount
	q.maxPktCount = DefaultPktCount
	q.lock = &sync.RWMutex{}
	q.cond = sync.NewCond(q.lock.RLocker())
	q.videoidx = -1
	return q
}

// SetMaxGopCount set MaxGopCount
func (q *Queue) SetMaxGopCount(n int) {
	q.lock.Lock()
	q.maxGOPCount = n
	q.lock.Unlock()
	return
}

// SetMaxPktCount set MaxPktCount
func (q *Queue) SetMaxPktCount(n int) {
	q.lock.Lock()
	q.maxPktCount = n
	q.lock.Unlock()
	return
}

// GetPktCount
func (q *Queue) GetPktCount() int {
	return q.curVideoCount + q.curAudioCount
}

// SetSID 设置流ID
func (q *Queue) SetSID(id string) {
	q.sid = id
	return
}

// WriteHeader write header
func (q *Queue) WriteHeader(data []av.CodecData) error {
	q.lock.Lock()

	datas := avutil.ConvertHeader(data)

	// 如果仅只有视频头或音频头
	if len(datas) == 1 && len(q.headers) > 0 {
		prevHeader := q.headers[len(q.headers)-1]
		lostHeaderType := av.HeaderTypeH264
		lostHeader := "video"
		if datas[0].Type == av.HeaderTypeH264 {
			lostHeaderType = av.HeaderTypeAAC
			lostHeader = "audio"
		}
		for _, data := range prevHeader.Datas {
			if data.Type == lostHeaderType {
				datas = append(datas, data)
				log.Info().Str("sid", q.sid).Str("lost_header", lostHeader).Msg("[Queue] repair lost header")
				break
			}
		}
	}

	duplicatedHeader := false
	for i := 0; i < len(q.headers); i++ {
		// 音频和视频的header可能会分别写入,这里做个简单的去重
		if q.headers[i].BeginAt == q.buf.Tail {
			q.headers[i] = Header{Datas: datas, BeginAt: q.buf.Tail}
			duplicatedHeader = true
			break
		}
	}
	if !duplicatedHeader {
		q.headers = append(q.headers, Header{Datas: datas, BeginAt: q.buf.Tail})
	}
	for i, data := range datas {
		if data.Type.IsVideo() {
			q.videoidx = i //TODO 这个用法在少数情况下，会导致部分数据被跳过
			break
		}
	}
	q.cond.Broadcast()

	q.lock.Unlock()

	log.Debug().Msg("[Queue] write header")

	return nil
}

// WriteTrailer write trailer
func (q *Queue) WriteTrailer() error {
	return nil
}

// Close After Close() called, all QueueCursor's ReadPacket will return io.EOF.
func (q *Queue) Close() (err error) {
	if q == nil {
		return
	}

	q.lock.Lock()

	q.closed = true
	q.cond.Broadcast()

	q.lock.Unlock()
	return
}

// WritePacket Put packet into buffer, old packets will be discared.
func (q *Queue) WritePacket(pkt av.Packet) error {
	q.lock.Lock()

	if len(q.headers) > 0 {
		pkt.HeaderBeginAt = int(q.headers[len(q.headers)-1].BeginAt)
	}

	q.buf.Push(pkt)

	if pkt.DataType == int8(flvio.TAG_VIDEO) {
		q.curVideoCount++
	} else if pkt.DataType == int8(flvio.TAG_AUDIO) {
		q.curAudioCount++
	}

	if pkt.DataType == int8(flvio.TAG_VIDEO) && pkt.IsKeyFrame {
		q.curGOPCount++
	}

	for q.buf.Count > 1 && (q.curGOPCount >= q.maxGOPCount || q.buf.Count >= q.maxPktCount) {
		pkt := q.buf.Pop()
		if pkt.DataType == int8(flvio.TAG_VIDEO) {
			q.curVideoCount--
		} else if pkt.DataType == int8(flvio.TAG_AUDIO) {
			q.curAudioCount--
		}
		if pkt.DataType == int8(flvio.TAG_VIDEO) && pkt.IsKeyFrame {
			q.curGOPCount--
		}
		if q.curGOPCount < q.maxGOPCount && q.buf.Count < q.maxPktCount {
			break
		}
	}
	//清理header
	clearPoint := len(q.headers) - 1
	for ; clearPoint >= 0; clearPoint-- {
		if q.buf.Head >= q.headers[clearPoint].BeginAt {
			break
		}
	}
	if clearPoint > 0 {
		q.headers = q.headers[clearPoint:]
	}

	q.cond.Broadcast()
	q.lock.Unlock()
	return nil
}

// QueueCursor Cursor of queue
type QueueCursor struct {
	que                *Queue
	pos                BufPos
	gotpos             bool
	readCount          int64
	preInited          bool
	id                 string
	sid                string
	StartOffset        int
	SkipFrameThreshold int
	curHeaderBeginAt   BufPos
	init               func(buf *Buf, videoidx int, startOffset int, adjustToLastKeyFrame bool) BufPos

	// P2P quickTime req
	TimeOffset       int
	initByTimeOffset func(buf *Buf, videoidx int, timeOffset int, adjustToLastKeyFrame bool) BufPos

	// LAS startPts req
	StartPts       int
	initByStartPts func(buf *Buf, videoidx int, startPts int, adjustToLastKeyFrame bool) BufPos

	// slice req
	EnableSlice        bool
	SliceStartId       uint32
	SliceSubstreamId   uint8
	SliceStreamBase    uint8
	curAtSliceId       uint32 // 当前指向的切片ID
	lastSendSliceId    uint32 // 上一次发送的切片ID
	lastSendSliceStamp uint64
	initSlice          func(buf *Buf, sliceStartId uint32, sliceSubstreamId uint8, sliceStreamBase uint8) (BufPos, uint32)
}

func (q *Queue) newCursor() *QueueCursor {
	return &QueueCursor{
		que:              q,
		curHeaderBeginAt: -1,
	}
}

// CursorByDelayedFrame 按帧偏移量初始化游标，对齐到关键帧
func (q *Queue) CursorByDelayedFrame(id, sid string, startOffset, skipFrameThreshold int) *QueueCursor {
	cursor := q.newCursor()
	cursor.id = id
	cursor.sid = sid
	cursor.StartOffset = startOffset
	cursor.SkipFrameThreshold = skipFrameThreshold
	cursor.init = func(buf *Buf, videoidx int, startOffset int, adjustToLastKeyFrame bool) BufPos {
		i := buf.Tail - 1
		delayedFrame := 0
		lastKeyFramePos := buf.Tail
		if videoidx != -1 {
			for ; buf.IsValidPos(i); i-- {
				pkt := buf.Get(i)
				if pkt.Idx == int8(videoidx) && pkt.IsKeyFrame {
					if delayedFrame >= startOffset {
						break
					}
					lastKeyFramePos = i
				}
				delayedFrame++
			}
		}

		if adjustToLastKeyFrame {
			if buf.IsValidPos(i) {
				return i
			}
			return lastKeyFramePos //这个位置仍然不可用，表示队列中没有关键帧,这种情况下会导致外层继续等待
		}
		//这里返回的可能是一个不可用位置
		return i
	}
	return cursor
}

// CursorBySliceReq 按切片请求参数，找到对应的位置
func (q *Queue) CursorBySliceReq(id, sid string, sliceStartId uint32, sliceSubstreamId, sliceStreamBase uint8) *QueueCursor {
	cursor := q.newCursor()
	cursor.id = id
	cursor.sid = sid
	cursor.EnableSlice = true
	cursor.SliceStartId = sliceStartId
	cursor.SliceSubstreamId = sliceSubstreamId
	cursor.SliceStreamBase = sliceStreamBase
	cursor.initSlice = func(buf *Buf, sliceStartId uint32, sliceSubstreamId uint8, sliceStreamBase uint8) (BufPos, uint32) {
		var minSliceId, maxSliceId uint32
		var minSlicePos, maxSlicePos BufPos
		//获取最大的sliceId
		for i := buf.Tail - 1; buf.IsValidPos(i); i-- {
			pkt := buf.Get(i)
			if pkt.IsVideo() {
				maxSliceId = pkt.SliceId
				// 找到前面的非视频帧，切片开始的音频帧
				for i = i - 1; buf.IsValidPos(i); i-- {
					pkt = buf.Get(i)
					if pkt.IsVideo() {
						maxSlicePos = i + 1
						break
					}
				}
				// 没找到切片的开始位置
				if maxSlicePos == 0 {
					maxSlicePos = buf.Head
				}
				break
			}
		}
		//获取最小的sliceId，用第2小的视频帧最为最小帧，保证切片的完整性（第一小的视频帧可能缺少音频帧）
		for i := buf.Head; buf.IsValidPos(i); i++ {
			pkt := buf.Get(i)
			if pkt.IsVideo() {
				minSliceId = pkt.SliceId + 1
				minSlicePos = i + 1
				break
			}
		}

		// 1.从最大帧开始发送
		// 没有带startID字段，则从最新的切片开始发送
		// startID比CDN最新的切片ID都大，则从最新的切片开始发送
		if sliceStartId == 0 || sliceStartId >= maxSliceId {
			return maxSlicePos, maxSliceId
		}
		// 2.从最小帧开始发送
		//startID比CDN最旧的切片ID都小，则从CDN最旧的切片开始发送
		if sliceStartId <= minSliceId {
			return minSlicePos, minSliceId
		}

		// 3.sliceStartId在中间
		pos := minSlicePos
		sliceId := sliceStartId
		for i := minSlicePos; buf.IsValidPos(i); i++ {
			pkt := buf.Get(i)
			if pkt.IsVideo() {
				if sliceStartId-1 == pkt.SliceId {
					pos = i + 1
					break
				}
				// 增大跳帧
				if sliceStartId < pkt.SliceId {
					pos = i + 1
					sliceId = pkt.SliceId
					break
				}
			}
		}
		return pos, sliceId
	}
	return cursor
}

func (q *Queue) Stat() *Stat {
	stat := &Stat{
		PktCount:   uint32(q.buf.Count),
		GopCount:   uint32(q.curGOPCount),
		VideoCount: uint32(q.curVideoCount),
		AudioCount: uint32(q.curAudioCount),
		HeadPos:    int(q.buf.Head),
		TailPos:    int(q.buf.Tail),
		Closed:     q.closed,
	}
	return stat
}

func (q *Queue) Format() string {
	res := fmt.Sprintf("queue maxGopCount:[%d], curGopCount[%d], pktNum[%d]", q.maxGOPCount, q.curGOPCount, q.buf.Count)
	return res
}

// Headers 返回队列中缓存的音视频header
func (q *QueueCursor) Headers() (cdata []av.CodecData, err error) {
	q.que.cond.L.Lock()
	defer q.que.cond.L.Unlock()
	if q.que.closed {
		err = io.EOF
		return
	}
	if q.curHeaderBeginAt == -1 {
		return
	}
	for q.que.headers == nil && !q.que.closed {
		q.que.cond.Wait()
	}
	var headerBeginAts []int
	if q.que.headers != nil && len(q.que.headers) > 0 {
		var header Header
		for _, h := range q.que.headers {
			headerBeginAts = append(headerBeginAts, int(h.BeginAt))
			if q.curHeaderBeginAt == h.BeginAt {
				header = h
				cdata = avutil.RevertHeader(header.Datas)
				break
			}
		}
	} else {
		err = io.EOF
	}
	log.Info().Str("id", q.id).Str("sid", q.sid).Ints("headerBeginAts", headerBeginAts).Int(
		"curHeaderBeginAt", int(q.curHeaderBeginAt)).Int("headers.len", len(cdata)).Err(
		err).Msg("[QueueCursor] read headers")
	return
}

func (q *QueueCursor) preInit() (err error) {
	buf := q.que.buf
	for !q.gotpos {
		if q.StartPts > 0 {
			q.pos = q.initByStartPts(buf, q.que.videoidx, q.StartPts, true)
		} else if q.TimeOffset > 0 {
			q.pos = q.initByTimeOffset(buf, q.que.videoidx, q.TimeOffset, true)
		} else {
			q.pos = q.init(buf, q.que.videoidx, q.StartOffset, false)
		}
		timeDelay := 0
		if buf.IsValidPos(q.pos) {
			timeDelay = int(util.TimeToTs(buf.Get(buf.Tail-1).Time) - util.TimeToTs(buf.Get(q.pos).Time))
		}
		log.Info().Str("id", q.id).Str("sid", q.sid).Int("pos", int(q.pos)).Int(
			"videoidx", q.que.videoidx).Int("head", int(buf.Head)).Int(
			"tail", int(buf.Tail)).Int("startOffset", q.StartOffset).Int(
			"timeOffset", q.TimeOffset).Int("startPts", q.StartPts).Int32(
			"posPts", util.TimeToTs(buf.Get(q.pos).Time)).Int("timeDelay", timeDelay).Msg("[QueueCursor] pre-init cursor")
		if buf.IsValidPos(q.pos) {
			q.gotpos = true
			q.preInited = true
			break
		}
		if q.que.closed {
			err = io.EOF
			break
		}
		q.que.cond.Wait()
	}
	return
}

func (q *QueueCursor) preInitSlice() (err error) {
	buf := q.que.buf
	for !q.gotpos {
		q.pos, q.curAtSliceId = q.initSlice(buf, q.SliceStartId, q.SliceSubstreamId, q.SliceStreamBase)
		log.Info().
			Str("id", q.id).
			Str("sid", q.sid).
			Int("pos", int(q.pos)).
			Int("videoidx", q.que.videoidx).
			Int("head", int(buf.Head)).
			Int("tail", int(buf.Tail)).
			Uint32("sliceStartId", q.SliceStartId).
			Uint32("curAtSliceId", q.curAtSliceId).
			Uint8("sliceSubstreamId", q.SliceSubstreamId).
			Uint8("sliceStreamBase", q.SliceStreamBase).
			Msg("[QueueCursor] pre-init slice cursor")
		if buf.IsValidPos(q.pos) {
			q.gotpos = true
			q.preInited = true
			break
		}
		if q.que.closed {
			err = io.EOF
			break
		}
		q.que.cond.Wait()
	}
	return
}

// readSlicePacket read substreamId slice ，skip other slice
func (q *QueueCursor) readSlicePacket() (pkt av.Packet, err error) {
	q.que.cond.L.Lock()
	buf := q.que.buf
	if !q.preInited {
		if err = q.preInitSlice(); err != nil {
			q.que.cond.L.Unlock()
			return
		}
	}
	for {
		if q.pos.LT(buf.Head) {
			q.que.lossPktCount += uint32(buf.Head - q.pos)
		}
		if q.pos.GT(buf.Tail) {
			q.pos = buf.Tail
			q.que.lossPktCount += uint32(q.pos - buf.Tail)
		}
		if q.pos.LT(buf.Head) {
			// 从最新的视频帧开始
			tmpPos := buf.Tail - 1
			for i := tmpPos; buf.IsValidPos(i); i-- {
				pkt := buf.Get(i)
				if pkt.IsVideo() {
					q.curAtSliceId = pkt.SliceId + 1
					q.pos = i + 1
					break
				}
			}
			if buf.IsValidPos(q.pos) {
				q.gotpos = true
			} else {
				q.gotpos = false
				if q.que.closed {
					err = io.EOF
					break
				}
				log.Error().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("head", int(buf.Head)).
					Int("tail", int(buf.Tail)).
					Int("pos", int(q.pos)).
					Int("delayframe", int(buf.Tail-q.pos)).
					Int("threshold", q.SkipFrameThreshold).
					Int64("readcount", q.readCount).
					Msg("[QueueCursor] re-init slice cursor pos invalid")

				q.que.cond.Wait()
				continue
			}
		}

		if buf.IsValidPos(q.pos) {
			pktTmp := buf.Get(q.pos)
			getPktFlag := false
			if q.SliceStreamBase == 0 || (q.curAtSliceId%uint32(q.SliceStreamBase)) == uint32(q.SliceSubstreamId) {
				pkt = pktTmp
				getPktFlag = true
				q.readCount++
				sendInterval := 0
				// 判断发送跳片
				if pktTmp.IsVideo() {
					streamBase := q.SliceStreamBase
					if streamBase == 0 {
						streamBase = 1
					}
					if q.lastSendSliceId != 0 && q.curAtSliceId != q.lastSendSliceId+uint32(streamBase) {
						log.Error().
							Str("id", q.id).
							Str("sid", q.sid).
							Uint32("lastSendSliceId", q.lastSendSliceId).
							Uint32("curAtSliceId", q.curAtSliceId).
							Msg("[QueueCursor] slice Jump")

					}
					q.lastSendSliceId = q.curAtSliceId
					// 计算切片发送间隔
					now := uint64(time.Now().UnixNano() / int64(time.Millisecond))
					if q.lastSendSliceStamp != 0 {
						sendInterval = int(now - q.lastSendSliceStamp)
					}
					q.lastSendSliceStamp = now
				}
				if sendInterval > 800 {
					log.Info().
						Str("id", q.id).
						Str("sid", q.sid).
						Int8("DataType", pktTmp.DataType).
						Uint32("SliceId", q.curAtSliceId).
						Uint8("substreamID", q.SliceSubstreamId).
						Uint8("streambase", q.SliceStreamBase).
						Uint16("frameCnt", pkt.SliceFrameCnt).
						Int("sendInterval", sendInterval).
						Msg("[QueueCursor] slice SendSlicePacket too long")

				}
			}
			if pktTmp.IsVideo() {
				q.curAtSliceId = pktTmp.SliceId + 1
			}
			q.pos++
			// 跳过当前切片
			if !getPktFlag {
				continue
			}

			if q.readCount%1000 == 0 {
				log.Info().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("pktcount", buf.Count).
					Int("pktsize", buf.Size).
					Int("head", int(buf.Head)).
					Int("tail", int(buf.Tail)).
					Int("pos", int(q.pos)).
					Int("delayed", int(buf.Tail-q.pos)).
					Int64("readcount", q.readCount).
					Uint32("curAtSliceId", q.curAtSliceId).
					Int("curHeaderBeginAt", int(q.curHeaderBeginAt)).
					Int("pkt.HeaderBeginAt", pkt.HeaderBeginAt).
					Int("pkt.Time", int(pkt.Time/time.Millisecond)).
					Msg("[QueueCursor] slice stats")

			}
			if pkt.HeaderBeginAt > int(q.curHeaderBeginAt) {
				pkt.HeaderChanged = true // 触发重新发送header
				log.Info().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("curHeaderBeginAt", int(q.curHeaderBeginAt)).
					Int("pkt.HeaderBeginAt", pkt.HeaderBeginAt).
					Int("pos", int(q.pos)).
					Int("pkt.Time", int(pkt.Time/time.Millisecond)).
					Msg("[QueueCursor] slice resend header")

				q.curHeaderBeginAt = BufPos(pkt.HeaderBeginAt)
			}
			break
		}
		if q.que.closed {
			err = io.EOF
			break
		}
		q.que.cond.Wait()
	}
	q.que.cond.L.Unlock()
	return
}

func (q *QueueCursor) readWholePacket() (pkt av.Packet, err error) {
	q.que.cond.L.Lock()
	buf := q.que.buf
	if !q.preInited {
		if err = q.preInit(); err != nil {
			return
		}
	}
	for {
		if q.pos.LT(buf.Head) {
			q.que.lossPktCount += uint32(buf.Head - q.pos)
		}
		if q.pos.GT(buf.Tail) {
			q.pos = buf.Tail
			q.que.lossPktCount += uint32(q.pos - buf.Tail)
		}
		if !q.gotpos || q.pos.LT(buf.Head) || (q.SkipFrameThreshold > 0 && buf.Tail-q.pos > BufPos(q.SkipFrameThreshold)) {
			//1 上一次跳帧没有得到有效位置
			//2 当pos落后于整个缓存队列(buf.Head),即使还没有达到阀值也要跳帧,防止配置不合理出现问题
			//3 pos落后帧数超过阀值
			oldPos := q.pos
			q.pos = q.init(buf, q.que.videoidx, q.StartOffset, true)
			log.Info().
				Str("id", q.id).
				Str("sid", q.sid).
				Int("oldpos", int(oldPos)).
				Int("pos", int(q.pos)).
				Int("head", int(buf.Head)).
				Int("tail", int(buf.Tail)).
				Int("videoidx", q.que.videoidx).
				Int("threshold", q.SkipFrameThreshold).
				Int("startoffset", q.StartOffset).
				Msg("[QueueCursor] re-init cursor")

			if buf.IsValidPos(q.pos) && q.pos > oldPos {
				q.gotpos = true
			} else {
				q.gotpos = false
				if q.que.closed {
					err = io.EOF
					break
				}
				log.Error().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("head", int(buf.Head)).
					Int("tail", int(buf.Tail)).
					Int("pos", int(q.pos)).
					Int("delayframe", int(buf.Tail-q.pos)).
					Int("threshold", q.SkipFrameThreshold).
					Int64("readcount", q.readCount).
					Msg("[QueueCursor] re-init cursor pos invalid")

				q.que.cond.Wait()
				continue
			}
		}

		if buf.IsValidPos(q.pos) {
			pkt = buf.Get(q.pos)
			q.pos++
			q.readCount++
			if q.readCount%1000 == 0 {
				log.Info().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("pktcount", buf.Count).
					Int("pktsize", buf.Size).
					Int("head", int(buf.Head)).
					Int("tail", int(buf.Tail)).
					Int("pos", int(q.pos)).
					Int("delayed", int(buf.Tail-q.pos)).
					Int("threshold", q.SkipFrameThreshold).
					Int64("readcount", q.readCount).
					Int("curHeaderBeginAt", int(q.curHeaderBeginAt)).
					Int("pkt.HeaderBeginAt", pkt.HeaderBeginAt).
					Int("pkt.Time", int(pkt.Time/time.Millisecond)).
					Msg("[QueueCursor] stats")

			}
			if pkt.HeaderBeginAt > int(q.curHeaderBeginAt) {
				pkt.HeaderChanged = true // 触发重新发送header
				log.Info().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("curHeaderBeginAt", int(q.curHeaderBeginAt)).
					Int("pkt.HeaderBeginAt", pkt.HeaderBeginAt).
					Int("pkt.Time", int(pkt.Time/time.Millisecond)).
					Msg("[QueueCursor] resend header")

				q.curHeaderBeginAt = BufPos(pkt.HeaderBeginAt)
			}
			break
		}
		if q.que.closed {
			err = io.EOF
			break
		}
		q.que.cond.Wait()
	}
	q.que.cond.L.Unlock()
	return
}

func (q *QueueCursor) SetTimeOffset(timeOffset int) {
	q.TimeOffset = timeOffset
	q.initByTimeOffset = func(buf *Buf, videoidx int, timeOffset int, adjustToLastKeyFrame bool) BufPos {
		i := buf.Tail - 1
		delayedFrame := 0
		lastKeyFramePos := buf.Tail
		if videoidx != -1 && buf.IsValidPos(i) {
			latestFramePts := util.TimeToTs(buf.Get(i).Time)
			for ; buf.IsValidPos(i); i-- {
				pkt := buf.Get(i)
				if pkt.Idx == int8(videoidx) && pkt.IsKeyFrame {
					if latestFramePts-util.TimeToTs(buf.Get(i).Time) >= int32(timeOffset) {
						break
					}
					lastKeyFramePos = i
				}
				delayedFrame++
			}
		}

		if adjustToLastKeyFrame {
			if buf.IsValidPos(i) {
				return i
			}
			return lastKeyFramePos //这个位置仍然不可用，表示队列中没有关键帧,这种情况下会导致外层继续等待
		}
		//这里返回的可能是一个不可用位置
		return i
	}
}

func (q *QueueCursor) SetStartPts(startPts int) {
	//当设定@startPts=0：非纯音频模式时，从最新的视频I帧开始拉流；纯音频模式时，从最新的音频帧开始拉流；
	//当设定@startPts>0：从pts大于等于@startPts的媒体帧开始拉流
	//当设定@startPts<0：拉取缓存长度为|@startPts|毫秒的媒体数据
	if startPts == 0 {
		return // do nothing
	} else if startPts < 0 {
		q.SetTimeOffset(-startPts)
		return
	}

	// startPts >0
	q.StartPts = startPts
	q.initByStartPts = func(buf *Buf, videoidx int, startPts int, adjustToLastKeyFrame bool) BufPos {
		i := buf.Head
		lastKeyFramePos := buf.Tail
		if videoidx != -1 && buf.IsValidPos(i) {
			for ; buf.IsValidPos(i); i++ {
				pkt := buf.Get(i)
				if pkt.Idx == int8(videoidx) && pkt.IsKeyFrame {
					if util.TimeToTs(buf.Get(i).Time) >= int32(startPts) {
						break
					}
					lastKeyFramePos = i
				}
			}
		}

		if adjustToLastKeyFrame {
			if buf.IsValidPos(i) {
				return i
			}
			return lastKeyFramePos //这个位置仍然不可用，表示队列中没有关键帧,这种情况下会导致外层继续等待
		}
		//这里返回的可能是一个不可用位置
		return i
	}
}

func (q *QueueCursor) UpdateOption(startOffset, skipFrameThreshold int) {
	q.que.cond.L.Lock()
	defer q.que.cond.L.Unlock()
	q.StartOffset = startOffset
	q.SkipFrameThreshold = skipFrameThreshold
}

// ReadPacket will not consume packets in Queue, it's just a cursor.
func (q *QueueCursor) ReadPacket() (av.Packet, error) {
	// 走切片拉流逻辑
	if q.EnableSlice {
		return q.readSlicePacket()
	}
	// 以前拉完整流逻辑
	return q.readWholePacket()
}

func (qc *QueueCursor) SeekToConfirmedPkt(confirmedPktTime time.Duration) {
	// just start from the latest pkt, in most cases, this has less performance cost

	buf := qc.que.buf
	idx := buf.Tail - 1

	// find latest keyframe video pkt that is earlier then confirmedPktTime
	for ; idx.GT(buf.Head); idx-- {
		if pkt := buf.Get(idx); pkt.EarlierThen(confirmedPktTime) && pkt.DataType == int8(flvio.TAG_VIDEO) && pkt.IsKeyFrame {
			// check if the queue is getting pure audio frames,
			// which would make transfer retransmit too many pure audios
			if confirmedPktTime-pkt.AbsoluteTime > minPureAudioDuration {
				qc.SeekToConfirmedAudioPkt(confirmedPktTime)
				return
			}
			qc.pos = idx
			return
		}
	}

	// otherwise, seek to the earliest keyframe, buf.Head
	for idx = buf.Head; idx.LT(buf.Tail); idx++ {
		if pkt := buf.Get(idx); pkt.DataType == int8(flvio.TAG_VIDEO) && pkt.IsKeyFrame {
			qc.pos = idx
			return
		}
	}
}

func (qc *QueueCursor) SeekToConfirmedAudioPkt(confirmedPktTime time.Duration) {
	buf := qc.que.buf
	idx := buf.Tail - 1

	// find latest audio pkt that is earlier then confirmedPktTime
	for ; idx.GT(buf.Head); idx-- {
		if pkt := buf.Get(idx); pkt.EarlierThen(confirmedPktTime) && pkt.DataType == int8(flvio.TAG_AUDIO) {
			qc.pos = idx
			return
		}
	}
	// not found , just seek to head
	qc.pos = buf.Head
}

func (qc *QueueCursor) Format() string {
	pkt := qc.que.buf.Get(qc.pos)
	return fmt.Sprintf("cursor: curPos[%d], pktTimestamp[%d], absoluteTimestamp[%d], isKeyFrame[%v]", qc.pos, util.TimeToTs(pkt.Time), util.TimeToTs(pkt.AbsoluteTime), pkt.IsKeyFrame)
}

func (qc *QueueCursor) Close() error {
	return nil
}
