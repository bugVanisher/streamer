package hls

import (
	"bytes"
	"container/list"
	"fmt"
	"github.com/rs/zerolog/log"
	"io/ioutil"
	"os"
	"sync"
	"time"
)

const (
	TSCacheNumMin = 3
)

var (
	ErrNoTsKey   = fmt.Errorf("no ts key for cache")
	ErrM3u8Empty = fmt.Errorf("m3u8 is empty")
)

type TSCache struct {
	id        string
	path      string
	hlsWindow int
	lock      sync.RWMutex
	ll        *list.List
	lm        map[string]TSItem

	// for record
	firstTsSeq       int
	firstTsTimeStamp int64
	tsDurationMax    int

	m3u8body *bytes.Buffer
	m3u8Lock sync.RWMutex
}

func NewTSCache(id, path string, hlsWindow int) *TSCache {
	return &TSCache{
		id:        id,
		path:      path,
		hlsWindow: hlsWindow,
		ll:        list.New(),
		lm:        make(map[string]TSItem),
		m3u8body:  bytes.NewBuffer(nil),
	}
}

func (c *TSCache) ID() string {
	return c.id
}

func (c *TSCache) IsRecord() bool {
	return c.hlsWindow == 0
}

func (c *TSCache) GetM3U8PlayList() ([]byte, error) {
	c.m3u8Lock.RLock()
	defer c.m3u8Lock.RUnlock()
	if len(c.m3u8body.Bytes()) == 0 {
		return nil, ErrM3u8Empty
	}
	return c.m3u8body.Bytes(), nil
}

// genRecordM3U8PlayList 直接落地ts，并更新m3u8
func (c *TSCache) genRecordM3U8PlayList(key string, item TSItem) {
	if c.tsDurationMax < item.Duration {
		c.tsDurationMax = item.Duration
	}
	if c.firstTsSeq == 0 {
		c.firstTsSeq = item.SeqNum
		c.firstTsTimeStamp = time.Now().Unix()
	}

	c.DumpTsFile(key, item)
	fmt.Fprintf(c.m3u8body, "#EXTINF:%.3f,\n%s\n", float64(item.Duration)/float64(1000), item.Name)
}

func (c *TSCache) genM3U8PlayList() {
	var seq int
	var getSeq bool
	var maxDuration int

	c.m3u8Lock.Lock()
	defer c.m3u8Lock.Unlock()
	c.m3u8body.Reset()
	w := bytes.NewBuffer(nil)

	if c.ll.Front() == nil {
		return
	}
	// 跳过第一个ts切片，m3u8第一个ts切片在ts请求到来时可能已经被淘汰了，导致404
	for e := c.ll.Front().Next(); e != nil; e = e.Next() {
		key := e.Value.(string)
		v, ok := c.lm[key]
		if ok {
			if v.Duration > maxDuration {
				maxDuration = v.Duration
			}
			if !getSeq {
				getSeq = true
				seq = v.SeqNum
			}
			fmt.Fprintf(w, "#EXTINF:%.3f,\n%s\n", float64(v.Duration)/float64(1000), v.Name)
		}
	}
	fmt.Fprintf(c.m3u8body,
		"#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-ALLOW-CACHE:NO\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n\n",
		maxDuration/1000+1, seq)
	c.m3u8body.Write(w.Bytes())
}

func (c *TSCache) SetItem(key string, item TSItem) {
	if c.IsRecord() {
		c.genRecordM3U8PlayList(key, item)
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()
	c.lm[key] = item
	c.ll.PushBack(key)
	// 计算总ts总时长
	totalDuration := 0
	for _, tsItem := range c.lm {
		totalDuration += tsItem.Duration
	}
	// 根据hlsWindow淘汰旧ts,可能淘汰多个ts
	for {
		if totalDuration <= c.hlsWindow || len(c.lm) <= TSCacheNumMin {
			break
		}
		e := c.ll.Front()
		c.ll.Remove(e)
		k := e.Value.(string)
		totalDuration = totalDuration - c.lm[k].Duration
		delete(c.lm, k)
		log.Info().Str("tsFile", k).Msg("[hls] deleteTS")
	}

	// 更新m3u8
	c.genM3U8PlayList()
}

func (c *TSCache) GetItem(key string) (TSItem, error) {
	c.lock.RLock()
	defer c.lock.RUnlock()
	item, ok := c.lm[key]
	if !ok {
		return item, ErrNoTsKey
	}
	return item, nil
}

func (c *TSCache) IsReady() bool {
	c.lock.RLock()
	defer c.lock.RUnlock()
	if len(c.lm) < TSCacheNumMin {
		return false
	}
	return true
}

func (c *TSCache) Reset() {
	c.lock.Lock()
	defer c.lock.Unlock()
	for e := c.ll.Front(); e != nil; e = e.Next() {
		c.ll.Remove(e)
	}
	c.lm = make(map[string]TSItem)
	c.m3u8body.Reset()
	c.firstTsSeq = 0
	c.tsDurationMax = 0
	log.Info().Msg("[hls] TSCache Reset")
}

func (c *TSCache) DumpM3U8PlayList() {
	if len(c.path) == 0 || c.m3u8body.Len() == 0 {
		return
	}

	w := bytes.NewBuffer(nil)
	fmt.Fprintf(w,
		"#EXTM3U\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-VERSION:3\n#EXT-X-ALLOW-CACHE:YES\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:%d\n\n",
		c.tsDurationMax/1000+1, c.firstTsSeq)
	w.Write(c.m3u8body.Bytes())
	w.WriteString("#EXT-X-ENDLIST\n")
	log.Info().Str("m3u8", c.m3u8body.String()).Msg("[hls] DumpM3U8PlayList")

	m3u8Path := fmt.Sprintf("%s/%s_%d.m3u8", c.path, c.id, c.firstTsTimeStamp)
	err := ioutil.WriteFile(m3u8Path, w.Bytes(), os.ModePerm)
	if err != nil {
		log.Error().Str("streamID", c.id).Str("m3u8Path", m3u8Path).Err(err).Msg("[hls] DumpM3U8PlayList WriteFile")
	}
	c.Reset()
	return
}

func (c *TSCache) DumpTsFile(key string, item TSItem) {
	if len(c.path) == 0 {
		return
	}
	os.MkdirAll(c.path+"/"+c.id, os.ModePerm)
	tsFile := c.path + "/" + c.id + "/" + key
	err := ioutil.WriteFile(tsFile, item.Data, 0666)
	if err != nil {
		log.Error().Str("streamID", c.id).
			Str("tsFile", tsFile).Err(err).Msg("[hls] DumpTsFile WriteFile")
	}
	return
}

type TSItem struct {
	Name     string
	SeqNum   int
	Duration int
	Data     []byte
}

func NewTSItem(name string, duration, seqNum int, b []byte) TSItem {
	var item TSItem
	item.Name = name
	item.SeqNum = seqNum
	item.Duration = duration
	item.Data = make([]byte, len(b))
	copy(item.Data, b)
	return item
}
