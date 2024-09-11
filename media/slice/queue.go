package slice

import (
	"fmt"
	"github.com/rs/zerolog/log"
	"io"
	"sync"
	"time"
)

const (
	// DefaultPktCount 默认bufqueue缓存pkt个数
	DefaultPktCount     = 10000 //10MB
	DefaultCacheTimeMax = 20000 //20s
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
	Datas   []Packet
	BeginAt BufPos
}

// Stat ...
type Stat struct {
	PktCount     uint32 `json:"pkt_count"`
	GopCount     uint32 `json:"gop_count"`
	PktDuration  int32  `json:"pkt_duration"`
	LossPktCount uint32 `json:"loss_pkt_count"`
	HeadPos      int    `json:"head_pos"`
	TailPos      int    `json:"tail_pos"`
	Closed       bool   `json:"closed"`
	MaxSliceId   uint64 `json:"max_slice_id"`
}

//Queue buffer queue
type Queue struct {
	buf     *Buf
	headers []Header
	lock    *sync.RWMutex
	cond    *sync.Cond
	closed  bool

	maxPktCount    int
	maxCacheTime   int32
	minPktDts      int32
	curPKtCount    int
	curGOPCount    int
	curPktDuration int32
	lossPktCount   uint32
	maxSliceId     uint64

	lastRecvSliceStamp uint64

	sid string
}

// NewQueue new a queue
func NewQueue() *Queue {
	q := &Queue{}
	q.buf = NewBuf()
	q.maxPktCount = DefaultPktCount
	q.maxCacheTime = DefaultCacheTimeMax
	q.lock = &sync.RWMutex{}
	q.cond = sync.NewCond(q.lock.RLocker())
	return q
}

// SetMaxPktCount set MaxPktCount
func (q *Queue) SetMaxPktCount(n int) {
	q.lock.Lock()
	q.maxPktCount = n
	q.lock.Unlock()
	return
}

// SetMaxCacheTime setMaxCacheTime
func (q *Queue) SetMaxCacheTime(n int) {
	q.lock.Lock()
	q.maxCacheTime = int32(n)
	q.lock.Unlock()
	return
}

// GetPktCount
func (q *Queue) GetPktCount() int {
	return q.curPKtCount
}

// SetSID 设置流ID
func (q *Queue) SetSID(id string) {
	q.sid = id
	return
}

// GetBySliceID for slice range
func (q *Queue) GetBySliceID(sliceID uint64) (Packet, error) {
	q.lock.Lock()
	defer q.lock.Unlock()
	self := q.buf
	if self.Head == self.Tail {
		return Packet{}, fmt.Errorf("buf is empty")
	}
	minSliceId := self.Get(self.Head).SliceId
	maxSliceId := self.Get(self.Tail - 1).SliceId
	if sliceID < minSliceId || sliceID > maxSliceId {
		return Packet{}, fmt.Errorf("sliceID not in buff")
	}

	// find by index offset
	diffPos := BufPos(sliceID - minSliceId)
	if self.Get(self.Head+diffPos).SliceId == sliceID {
		return self.Get(self.Head + diffPos), nil
	}

	// exception case
	for i := self.Head; self.IsValidPos(i); i++ {
		if self.Get(i).SliceId == sliceID {
			return self.Get(i), nil
		}
	}

	return Packet{}, fmt.Errorf("sliceID not found")
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

func (q *Queue) IsClosed() bool {
	return q.closed
}

func (q *Queue) WriteHeader(datas []Packet) error {
	q.lock.Lock()
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
	q.cond.Broadcast()
	q.lock.Unlock()

	log.Info().Str("sid", q.sid).Int("headerLen", len(q.headers)).Int("dataLen", len(datas)).Msg("[Queue] write header")
	return nil
}

// WritePacket Put packet into buffer, old packets will be discared.
func (q *Queue) WritePacket(pkt Packet) error {
	q.lock.Lock()

	if len(q.headers) > 0 {
		pkt.HeaderBeginAt = int(q.headers[len(q.headers)-1].BeginAt)
	}

	// 过滤重复写入
	if q.maxSliceId > 0 && pkt.SliceId <= q.maxSliceId {
		q.lock.Unlock()
		return nil
	}

	// 计算切片接受间隔
	var recvInterval int
	now := uint64(time.Now().UnixNano() / int64(time.Millisecond))
	if q.lastRecvSliceStamp != 0 {
		recvInterval = int(now - q.lastRecvSliceStamp)
	}
	q.lastRecvSliceStamp = now
	if recvInterval > 800 {
		log.Info().Str("sid", q.sid).Uint8("SliceType", pkt.SliceType).Uint8("posFlag", pkt.PosFlag).
			Uint64("SliceId", pkt.SliceId).Int("recInterval", recvInterval).Msg("[Queue] RecvSlicePacket too long")
	}

	q.buf.Push(pkt)
	q.curPKtCount++
	q.maxSliceId = pkt.SliceId
	if pkt.SliceType == SLICE_TYPE_VIDEO && pkt.FrameType == SLICE_FRAME_TYPE_IDR && pkt.PosFlag == SLICE_POSFLAG_START {
		q.curGOPCount++
	}

	for q.buf.Count > 1 && (q.buf.Count >= q.maxPktCount || (pkt.FrameDts > 0 && pkt.FrameDts-q.minPktDts > q.maxCacheTime)) {
		tmpPkt := q.buf.Pop()
		if pkt.FrameDts > 0 && tmpPkt.FrameDts > q.minPktDts {
			q.minPktDts = tmpPkt.FrameDts
			q.curPktDuration = pkt.FrameDts - q.minPktDts
		}
		if tmpPkt.SliceType == SLICE_TYPE_VIDEO && tmpPkt.FrameType == SLICE_FRAME_TYPE_IDR && tmpPkt.PosFlag == SLICE_POSFLAG_START {
			q.curGOPCount--
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

// WriteTrailer write trailer
func (q *Queue) WriteTrailer() error {
	return nil
}

func (q *Queue) Stat() *Stat {
	stat := &Stat{
		PktCount:    uint32(q.buf.Count),
		GopCount:    uint32(q.curGOPCount),
		PktDuration: q.curPktDuration,
		HeadPos:     int(q.buf.Head),
		TailPos:     int(q.buf.Tail),
		Closed:      q.closed,
		MaxSliceId:  q.maxSliceId,
	}
	return stat
}

// QueueCursor Cursor of queue
type QueueCursor struct {
	que       *Queue
	pos       BufPos
	gotpos    bool
	readCount int64
	preInited bool
	id        string
	sid       string

	// P2P quickTime req
	TimeOffset       int
	initByTimeOffset func(buf *Buf, timeOffset int, adjustToLastKeyFrame bool) (BufPos, uint64, int32)

	// slice req
	SliceStartId       uint64
	SliceSubstreamId   []uint8
	SliceStreamBase    uint8
	curAtSliceId       uint64 // 当前指向的切片ID
	lastSendSliceStamp uint64
	curHeaderBeginAt   BufPos
	initSlice          func(buf *Buf, sliceStartId uint64) (BufPos, uint64)
}

func (q *Queue) newCursor() *QueueCursor {
	return &QueueCursor{
		que:              q,
		curHeaderBeginAt: -1,
	}
}

// CursorBySliceReq 按切片请求参数，找到对应的位置
func (q *Queue) CursorBySliceReq(id, sid string, sliceStartId uint64, sliceSubstreamId []uint8, sliceStreamBase uint8) *QueueCursor {
	cursor := q.newCursor()
	cursor.id = id
	cursor.sid = sid
	cursor.SliceStartId = sliceStartId
	cursor.SliceSubstreamId = sliceSubstreamId
	cursor.SliceStreamBase = sliceStreamBase
	cursor.initSlice = func(buf *Buf, sliceStartId uint64) (BufPos, uint64) {
		var minSliceId, maxSliceId uint64
		var minSlicePos, maxSlicePos BufPos
		maxSlicePos = buf.Tail - 1
		if buf.IsValidPos(maxSlicePos) {
			maxSliceId = buf.Get(maxSlicePos).SliceId
		}
		minSlicePos = buf.Head
		if buf.IsValidPos(minSlicePos) {
			minSliceId = buf.Get(minSlicePos).SliceId
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
			// 大于等于是为了适配跳帧场景
			if pkt.SliceId >= sliceStartId {
				pos, sliceId = i, pkt.SliceId
				break
			}
		}
		return pos, sliceId
	}
	return cursor
}

func (q *QueueCursor) SetTimeOffset(timeOffset int) {
	q.TimeOffset = timeOffset
	q.initByTimeOffset = func(buf *Buf, timeOffset int, adjustToLastKeyFrame bool) (BufPos, uint64, int32) {
		isNegative := false
		if timeOffset < 0 {
			isNegative = true
			timeOffset = -timeOffset
		}
		i := buf.Tail - 1
		delayedFrame := 0
		lastKeyFramePos := buf.Tail
		var latestFrameDts int32
		if buf.IsValidPos(i) {
			latestFrameDts = buf.Get(i).FrameDts
			for ; buf.IsValidPos(i); i-- {
				pkt := buf.Get(i)
				if pkt.FrameType == SLICE_FRAME_TYPE_IDR && pkt.PosFlag == SLICE_POSFLAG_START {
					if latestFrameDts-buf.Get(i).FrameDts >= int32(timeOffset) {
						//timeOffset < 0 表示：有可能从紧挨着的前一个关键帧，或者后一个关键帧开始发，关键看哪个离的近
						if isNegative && lastKeyFramePos < buf.Tail {
							tmp1 := latestFrameDts - buf.Get(i).FrameDts - int32(timeOffset)
							tmp2 := int32(timeOffset) - (latestFrameDts - buf.Get(lastKeyFramePos).FrameDts)
							if tmp2 < tmp1 {
								return lastKeyFramePos, buf.Get(lastKeyFramePos).SliceId, tmp2
							}
						}
						break
					}
					lastKeyFramePos = i
				}
				delayedFrame++
			}
		}

		if adjustToLastKeyFrame {
			if buf.IsValidPos(i) {
				return i, buf.Get(i).SliceId, latestFrameDts - buf.Get(i).FrameDts
			}
			if buf.IsValidPos(lastKeyFramePos) {
				return lastKeyFramePos, buf.Get(lastKeyFramePos).SliceId, latestFrameDts - buf.Get(lastKeyFramePos).FrameDts
			}
			//这个位置仍然不可用，表示队列中没有关键帧,这种情况下会导致外层继续等待
			return lastKeyFramePos, 0, 0
		}

		if buf.IsValidPos(i) {
			return i, buf.Get(i).SliceId, latestFrameDts - buf.Get(i).FrameDts
		}

		//这里返回的可能是一个不可用位置
		return i, 0, 0
	}
}

// Headers 返回队列中缓存的音视频header
func (q *QueueCursor) Headers() (cdata []Packet, err error) {
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
				cdata = header.Datas
				break
			}
		}
	} else {
		err = io.EOF
	}
	log.Info().
		Str("id", q.id).
		Str("sid", q.sid).
		Ints("headerBeginAts", headerBeginAts).
		Int("curHeaderBeginAt", int(q.curHeaderBeginAt)).
		Int("headers.len", len(cdata)).
		Err(err).
		Msg("[QueueCursor] read slice headers")
	return
}

func (q *QueueCursor) preInitSlice() (err error) {
	buf := q.que.buf
	for !q.gotpos {
		var timeDelay int32
		if q.TimeOffset != 0 {
			q.pos, q.curAtSliceId, timeDelay = q.initByTimeOffset(buf, q.TimeOffset, true)
		} else if q.SliceStreamBase == 0 && q.SliceStartId == 0 {
			q.pos, q.curAtSliceId, timeDelay = q.initByTimeOffset(buf, q.TimeOffset, true)
		} else {
			q.pos, q.curAtSliceId = q.initSlice(buf, q.SliceStartId)
		}
		log.Info().
			Str("id", q.id).
			Str("sid", q.sid).
			Int("pos", int(q.pos)).
			Int("head", int(buf.Head)).
			Int("tail", int(buf.Tail)).
			Uint64("sliceStartId", q.SliceStartId).
			Uint64("curAtSliceId", q.curAtSliceId).
			Uints8("substreamId", q.SliceSubstreamId).
			Uint8("sliceStreamBase", q.SliceStreamBase).
			Int("TimeOffset", q.TimeOffset).
			Int32("timeDelay", timeDelay).
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

// ReadPacket will not consume packets in Queue, it's just a cursor.
func (q *QueueCursor) ReadPacket() (pkt Packet, err error) {
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
			// 跳到合法位置， TODO
			q.pos = buf.Head + 1
			q.curAtSliceId = buf.Get(q.pos).SliceId

			if buf.IsValidPos(q.pos) {
				q.gotpos = true
			} else {
				q.gotpos = false
				if q.que.closed {
					err = io.EOF
					break
				}
				log.Err(fmt.Errorf("[QueueCursor] re-init slice cursor pos invalid")).
					Str("id", q.id).
					Str("sid", q.sid).
					Int("head", int(buf.Head)).
					Int("tail", int(buf.Tail)).
					Int("pos", int(q.pos)).
					Int("delayframe", int(buf.Tail-q.pos)).
					Int64("readcount", q.readCount).
					Msg("")

				q.que.cond.Wait()
				continue
			}
		}

		if buf.IsValidPos(q.pos) {
			pktTmp := buf.Get(q.pos)
			getPktFlag := false
			if q.SliceStreamBase == 0 || q.IsReqSubStreamId(pktTmp.SliceId) {
				pkt = pktTmp
				getPktFlag = true
				q.readCount++
				sendInterval := 0

				// 计算切片发送间隔
				now := uint64(time.Now().UnixNano() / int64(time.Millisecond))
				if q.lastSendSliceStamp != 0 {
					sendInterval = int(now - q.lastSendSliceStamp)
				}
				q.lastSendSliceStamp = now

				if sendInterval > 800 {
					log.Info().
						Str("id", q.id).
						Str("sid", q.sid).
						Uint8("SliceType", pktTmp.SliceType).
						Uint64("SliceId", pktTmp.SliceId).
						Uints8("substreamId", q.SliceSubstreamId).
						Uint8("streambase", q.SliceStreamBase).
						Int("sendInterval", sendInterval).
						Msg("[QueueCursor] slice SendSlicePacket too long")

				}
			}

			if q.SliceStreamBase == 0 && pktTmp.SliceId != q.curAtSliceId {
				log.Info().
					Str("id", q.id).
					Str("sid", q.sid).
					Uint64("curSliceId", q.curAtSliceId).
					Uint64("nextSliceId", pktTmp.SliceId).
					Msg("[QueueCursor] slice SendSlicePacket Jump")

			}

			q.curAtSliceId = pktTmp.SliceId + 1
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
					Uint64("curAtSliceId", q.curAtSliceId).
					Msg("[QueueCursor] slice stats")

			}
			if pkt.HeaderBeginAt > int(q.curHeaderBeginAt) {
				pkt.HeaderChanged = true // 触发重新发送slice header
				log.Info().
					Str("id", q.id).
					Str("sid", q.sid).
					Int("curHeaderBeginAt", int(q.curHeaderBeginAt)).
					Int("pkt.HeaderBeginAt", pkt.HeaderBeginAt).
					Int("pos", int(q.pos)).
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

func (qc *QueueCursor) Format() string {
	pkt := qc.que.buf.Get(qc.pos)
	return fmt.Sprintf("cursor: curPos[%d], pktTimestamp[%d]", qc.pos, pkt.FrameDts)
}

func (qc *QueueCursor) Close() error {
	return nil
}

func (q *QueueCursor) IsReqSubStreamId(sliceId uint64) bool {
	subStreamId := uint8(sliceId % uint64(q.SliceStreamBase))
	for _, subId := range q.SliceSubstreamId {
		if subStreamId == subId {
			return true
		}
	}

	return false
}
