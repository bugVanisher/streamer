package utils

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	txPrefix = "33872_"
)

// ReplaceURLHost 替换url中的host
func ReplaceURLHost(rawurl string, host string) string {
	u, _ := url.Parse(rawurl)
	u.Host = host
	ss := strings.Split(u.Path, "/")
	if len(ss) == 4 {
		ss = ss[2:]
	}
	u.Path = strings.Join(ss, "/")
	return u.String()
}

// SetSPEHostEmpty 删除url中的speHost参数
func SetSPEHostEmpty(rawurl string) string {
	u, _ := url.Parse(rawurl)
	values := u.Query()
	values.Del("speHost")
	u.RawQuery = strings.ReplaceAll(values.Encode(), "%3A", ":")
	return u.String()
}

func RebuildURLPath(path string) string {
	ss := strings.Split(path, "/")
	if len(ss) == 4 {
		ss = ss[2:]
	}
	str := "/" + strings.Join(ss, "/")
	return strings.ReplaceAll(str, "//", "/")
}

// ReplaceURLProtoHost 替换url中的proto和host
func ReplaceURLProtoHost(rawurl, proto, host string) string {
	u, _ := url.Parse(rawurl)
	u.Host = host
	u.Scheme = proto
	return u.String()
}

// TimeToTs duration to timestamp
func TimeToTs(tm time.Duration) int32 {
	return int32(tm / time.Millisecond)
}

// PtsToTime video pts to duration
func PtsToTime(pts int64) time.Duration {
	// NOTICE:
	// time = timestamp(millisecond) * time.Millisecond
	// pts = timestamp(millisecond) * 90
	return time.Duration(pts/90) * time.Millisecond
}

// MSTimeString 返回当前时间字符串,精确到毫秒,格式yyyymmddhhmiss.000
func MSTimeString() string {
	return time.Now().Format("2006-01-02 15:04:05.000")
}

// RepairHostWithPort1935 ...
func RepairHostWithPort1935(host string) string {
	if _, _, err := net.SplitHostPort(host); err != nil {
		host += ":1935"
	}
	return host
}

// PeelOffPort1935 截取IP:1935为IP
func PeelOffPort1935(host string) string {
	if h, port, err := net.SplitHostPort(host); err == nil {
		if port == "1935" {
			return h
		}
	}
	return host
}

func GetHackStreamID(tcUrl string) string {
	u, err := url.Parse(tcUrl)
	if err != nil {
		return ""
	}
	ss := strings.Split(u.Path, "/")
	if len(ss) != 3 {
		return ""
	}
	return ss[2]
}

// TimingPause 带有计时功能的暂停
func TimingPause(pause time.Duration, cost *time.Duration) {
	time.Sleep(pause)
	*cost += pause
}

// ContextDone 判断一个context是否已经结束/取消/超时
func ContextDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// PanicRecover panic恢复处理
func PanicRecover() {
	if r := recover(); r != nil {
		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
	}
}

// PanicRecoverWithInfo panic恢复处理
func PanicRecoverWithInfo(info string) {
	if r := recover(); r != nil {
		const size = 64 << 10
		buf := make([]byte, size)
		buf = buf[:runtime.Stack(buf, false)]
	}
}

// ExtractStreamID 从streamName抽取streamID
func ExtractStreamID(streamName string) string {
	if strings.HasPrefix(streamName, txPrefix) {
		return streamName[len(txPrefix):]
	}
	return streamName
}

// GetCidFromStreamID 从streamID获取cid
func GetCidFromStreamID(sid string) string {
	sr := strings.Split(sid, "/")
	if len(sr) == 2 {
		sid = sr[1]
	}
	ss := strings.Split(sid, "-")
	if len(ss) >= 4 {
		sv := strings.Split(ss[0], "_")
		if len(sv) == 1 {
			return sv[0]
		}
		if len(sv) == 2 {
			return sv[1]
		}
	}
	return ""
}

// HTTPPost 对http post请求的包装
func HTTPPost(cli *http.Client, url, data string) (string, error) {
	req, err := http.NewRequest("POST", url, strings.NewReader(data))
	if err != nil {
		return "", err
	}

	response, err := cli.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", err
	}

	content, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return "", err
	}

	return string(content), nil
}

// GetModuleName 从fullModuleName获取moduleName，考虑将httpflv模块后缀去掉
func GetModuleName(fullModuleName string) string {
	ss := strings.Split(fullModuleName, "-")
	if len(ss) != 4 {
		return ""
	}
	sname := ss[1]
	idx := strings.Index(sname, "httpflv")
	if idx > 0 {
		sname = sname[:idx]
	}
	return sname
}

// GetStreamInfo ...
func GetStreamInfo(sid string) (cid string, roomID string, sessionID string) {
	sr := strings.Split(sid, "/")
	if len(sr) == 2 {
		sid = sr[1]
	}
	ss := strings.Split(sid, "-")
	if len(ss) != 4 {
		return
	}

	sv := strings.Split(ss[0], "_")
	if len(sv) >= 1 {
		cid = sv[len(sv)-1]
	}

	roomID = ss[2]

	sv = strings.Split(ss[3], "_")
	if len(sv) >= 1 {
		sessionID = sv[0]
	}
	return
}

func ExtractLiveInfo(rawurl string) (host, app, sid string, err error) {
	u, e := url.Parse(rawurl)
	if e != nil {
		err = e
		return
	}
	var path string
	pos := strings.LastIndex(u.Path, ".")
	if pos < 0 {
		path = u.Path
	} else {
		path = u.Path[0:pos]
	}
	host = PeelOffPort1935(u.Host)
	ps := strings.Split(path, "/")
	if len(ps) == 3 { // e.g. /live/stream
		app = ps[1]
		sid = ps[2]
	} else if len(ps) == 4 { // e.g. /domain/live/stream
		host = ps[1]
		app = ps[2]
		sid = ps[3]
	} else {
		err = errors.New("invalid path")
		return
	}
	return
}

func GetUrlFileSuffix(rawurl string) (suffix string, err error) {
	u, e := url.Parse(rawurl)
	if e != nil {
		err = e
		return
	}
	pos := strings.LastIndex(u.Path, ".")
	if pos < 0 {
		err = errors.New("invalid path")
		return
	}

	suffix = u.Path[pos:]
	return
}

// GetTsInfoFromTsUrl sg-live-43287-96716-1626688779.ts
func GetTsInfoFromTsUrl(rawurl string) (sid, fileName string, err error) {
	u, e := url.Parse(rawurl)
	if e != nil {
		err = e
		return
	}
	pos := strings.LastIndex(u.Path, "/")
	if pos < 0 || pos >= len(u.Path)-1 {
		err = errors.New("invalid path")
		return
	}

	fileName = u.Path[pos+1:]
	tsPos := strings.LastIndex(fileName, "-")
	if tsPos < 0 {
		err = errors.New("invalid path")
		return
	}
	sid = fileName[0:tsPos]
	return
}

func TimeNowMillisecond() uint64 {
	return uint64(time.Now().UnixNano() / int64(time.Millisecond))
}

func IsValidURL(s string) bool {
	if s == "" {
		return false
	}
	_, err := url.Parse(s)
	if err != nil {
		return false
	}
	return true
}

func FileExists(path string) bool {
	_, err := os.Stat(path)
	if err != nil {
		if os.IsExist(err) {
			return true
		}
		return false
	}
	return true
}

func GetSubPath(path string) []string {
	subDirs, err := ioutil.ReadDir(path)
	if err != nil {
		return nil
	}
	subPaths := make([]string, 0, len(subDirs))
	for _, subPath := range subDirs {
		if subPath.IsDir() {
			subPaths = append(subPaths, subPath.Name())
		}
	}

	return subPaths
}

type timer struct {
	threshold time.Duration
	accTime   time.Duration
}

func NewTimer(threshold time.Duration) *timer {
	return &timer{threshold: threshold}
}

func (t *timer) Sleep(d time.Duration) bool {
	time.Sleep(d)
	if t.threshold < 0 {
		return false
	}
	t.accTime += d
	if t.accTime > t.threshold {
		return true
	}
	return false
}

type SliceRequest struct {
	StartSliceId uint64
	SubstreamId  []uint8
	StreamBase   uint8
}

const (
	KSliceParamStartId     = "startID"
	KSliceParamSubstreamId = "substreamID"
	KSliceParamStreamBase  = "streambase"
	KSliceParamSliceRange  = "sliceRange"
	KSliceParamQuickOffset = "quickOffset"
)

func ParseHttpSliceReq(r *http.Request) (SliceRequest, error) {
	req := SliceRequest{}
	params := r.URL.Query()
	startIdStr := params.Get(KSliceParamStartId)
	substreamIdStr := params.Get(KSliceParamSubstreamId)
	streambaseStr := params.Get(KSliceParamStreamBase)
	if startIdStr != "" {
		tmp, err := strconv.ParseUint(startIdStr, 10, 64)
		if err != nil {
			return req, err
		}
		req.StartSliceId = tmp
	}
	if streambaseStr != "" {
		tmp, err := strconv.ParseUint(streambaseStr, 10, 64)
		if err != nil {
			return req, err
		}
		req.StreamBase = uint8(tmp)
	}
	if substreamIdStr != "" {
		subIdSpilts := strings.Split(substreamIdStr, ",")
		for _, subIdStr := range subIdSpilts {
			tmp, err := strconv.ParseUint(subIdStr, 10, 64)
			if err != nil {
				return req, err
			}
			if uint8(tmp) >= req.StreamBase {
				continue
			}
			req.SubstreamId = append(req.SubstreamId, uint8(tmp))
		}
	}

	if req.StreamBase != 0 && len(req.SubstreamId) == 0 {
		return req, fmt.Errorf("param substream %v >= streambase %d", substreamIdStr, req.StreamBase)
	}

	return req, nil
}

//ParseHttpSliceRangeReq param: startID=xx&sliceRange=x,x-y,x
func ParseHttpSliceRangeReq(r *http.Request) ([]uint64, error) {
	var startSliceId uint64
	sliceIDArray := make([]uint64, 0, 32)
	params := r.URL.Query()
	startIdStr := params.Get(KSliceParamStartId)
	sliceRangeStr := params.Get(KSliceParamSliceRange)

	if startIdStr != "" {
		tmp, err := strconv.ParseUint(startIdStr, 10, 64)
		if err != nil {
			return nil, err
		}
		startSliceId = tmp
	}

	rangeSpilts := strings.Split(sliceRangeStr, ",")
	for _, sliceStr := range rangeSpilts {
		splits := strings.Split(sliceStr, "-")
		if len(splits) > 2 {
			continue
		}
		if len(splits) == 2 {
			tmp1, err := strconv.ParseUint(splits[0], 10, 64)
			if err != nil {
				continue
			}
			tmp2, err := strconv.ParseUint(splits[1], 10, 64)
			if err != nil {
				continue
			}
			for tmp1 <= tmp2 {
				sliceIDArray = append(sliceIDArray, tmp1+startSliceId)
				tmp1++
			}
		}
		if len(splits) == 1 {
			tmp, err := strconv.ParseUint(splits[0], 10, 64)
			if err != nil {
				continue
			}
			sliceIDArray = append(sliceIDArray, tmp+startSliceId)
		}
	}

	return sliceIDArray, nil
}

func RemoveSliceParam(r *http.Request) string {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return ""
	}

	values.Del(KSliceParamStartId)
	values.Del(KSliceParamSubstreamId)
	values.Del(KSliceParamStreamBase)
	values.Del(KSliceParamSliceRange)
	values.Del(KSliceParamQuickOffset)

	return values.Encode()
}

func IsSliceSubstreamReq(srcUrl string) bool {
	return strings.Contains(srcUrl, KSliceParamStreamBase+"=")
}

func IsSliceRangeReq(srcUrl string) bool {
	return strings.Contains(srcUrl, KSliceParamSliceRange)
}

func Uint64ToBytes(i uint64) []byte {
	var buf = make([]byte, 8)
	binary.BigEndian.PutUint64(buf, i)
	return buf
}

func BytesToUint64(buf []byte) uint64 {
	return binary.BigEndian.Uint64(buf)
}

func Uint16ToBytes(i uint16) []byte {
	var buf = make([]byte, 2)
	binary.BigEndian.PutUint16(buf, i)
	return buf
}

func BytesToUint16(buf []byte) uint16 {
	return binary.BigEndian.Uint16(buf)
}

func Uint32ToBytes(i uint32) []byte {
	var buf = make([]byte, 4)
	binary.BigEndian.PutUint32(buf, i)
	return buf
}

func BytesToUint32(buf []byte) uint32 {
	return binary.BigEndian.Uint32(buf)
}

//判断是否是源流
//TODO: 配置转码模板判断是否为转码流
func IsSrcStreamName(sid string) bool {
	ss := strings.Split(sid, "_")
	if len(ss) < 2 {
		return true
	}
	return false
}

//获取源流流名称
func GetSrcStreamName(sid string) string {
	ss := strings.Split(sid, "_")
	return ss[0]
}
