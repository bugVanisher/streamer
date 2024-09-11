package rtmp

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// Debuger debug对象，记录任务的debug信息
type Debuger struct {
	taskID         string
	enabled        bool     //debug模式开关, 为true时开启
	debugFileName  string   //debug信息保存文件
	debugDuration  int64    //debug时长,单位秒
	debugStartTime int64    //debug开始时间,时间戳秒
	debugFile      *os.File //debug文件
	debugLock      sync.Mutex
}

// NewDebuger 创建debuger
func NewDebuger(taskID string) *Debuger {
	return &Debuger{
		taskID:  taskID,
		enabled: false,
	}
}

// Enabled debug开关是否打开
func (t *Debuger) Enabled() bool {
	if t == nil {
		return false
	}
	return t.enabled
}

// StartDebug 开启debug功能, 需要设定输出文件和debug时长, 如果已经在debug模式则忽略本次调用
func (t *Debuger) StartDebug(debugFileName string, debugDuration int64) bool {
	if t == nil {
		return false
	}
	t.debugLock.Lock()
	defer t.debugLock.Unlock()
	if t.enabled {
		return true
	}
	t.debugStartTime = time.Now().Unix()
	t.debugFileName = debugFileName
	t.debugDuration = debugDuration
	//打开文件
	var err error
	if t.debugFile != nil {
		t.debugFile.Close()
	}
	t.debugFile, err = os.Create(t.debugFileName)
	if err != nil {
		t.debugFile.Close()
		return false
	}
	t.enabled = true
	return true
}

// StopDebug 停止debug
func (t *Debuger) StopDebug() {
	if t == nil {
		return
	}
	t.debugLock.Lock()
	defer t.debugLock.Unlock()
	if !t.enabled {
		return
	}
	t.enabled = false
	if t.debugFile != nil {
		t.debugFile.Close()
	}
	return
}

// Debug 写入debug信息
func (t *Debuger) Debug(format string, args ...interface{}) {
	if t == nil {
		return
	}
	if !t.enabled || t.debugFile == nil {
		return
	}

	msg := fmt.Sprintf(time.Now().Format("2006-01-02 15:04:05.000")+" "+format+"\n", args...)
	t.debugFile.Write([]byte(msg))
	if t.debugDuration > 0 && time.Now().Unix() >= t.debugStartTime+t.debugDuration {
		t.StopDebug()
	}
	return
}
