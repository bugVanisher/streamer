package statistics

import (
	"fmt"
	"time"
)

/*
周期统计工具,滚动统计周期内的数值,统计周期精确到秒

TODO 在统计初期，没有完整周期数据的时候，统计的平均值会偏小，待优化
TODO 目前只支持一写多读
*/

// PeriodicStatistic 周期统计工具,滚动统计周期内数据的最大、最小、平均值
type PeriodicStatistic struct {
	gridNum    int64
	gridPeriod int64
	dataGrid   []int64

	avg int64
	max int64
	min int64
	sum int64

	lastIdx      int64
	lastStatTime int64
}

const (
	DefaultStatGridNum = int64(5)
)

// NewPeriodicStatistic 创建周期统计对象, gridNum统计格子数量, gridPeriod格子时间长度,单位秒
func NewPeriodicStatistic(gridNum, gridPeriod int64) *PeriodicStatistic {
	return &PeriodicStatistic{
		gridNum:      gridNum + 1,
		gridPeriod:   gridPeriod,
		dataGrid:     make([]int64, gridNum+1),
		lastIdx:      0,
		lastStatTime: 0,
	}
}

func (rcv *PeriodicStatistic) expired() bool {
	return time.Now().Unix() > rcv.lastStatTime+rcv.gridNum*rcv.gridPeriod
}

// Stat 添加统计值
func (rcv *PeriodicStatistic) Stat(val int64) {
	now := time.Now().Unix()
	idx := now % (rcv.gridNum * rcv.gridPeriod) / rcv.gridPeriod

	if now >= rcv.lastStatTime+rcv.gridNum*rcv.gridPeriod {
		//1 本次统计距上次已经超时 清空所有数据
		for i := int64(0); i < rcv.gridNum; i++ {
			rcv.dataGrid[i] = 0
		}
		rcv.dataGrid[idx] = val
		rcv.sum = val
		rcv.max = val
		rcv.min = val
		rcv.lastIdx = idx
		rcv.avg = rcv.calcAvg()
		rcv.lastStatTime = now
		return
	}
	if idx == rcv.lastIdx && now-rcv.lastStatTime <= rcv.gridPeriod {
		//2 跟上次统计落在同个格子
		rcv.dataGrid[idx] += val
		rcv.sum += val
		rcv.avg = rcv.calcAvg()
		if val > rcv.max {
			rcv.max = val
		}
		if val < rcv.min {
			rcv.min = val
		}
		rcv.lastStatTime = now
		return
	}
	//3 当前格子跟上一次不同，中间可能跳过若干个格子
	virtualPos := idx
	if virtualPos <= rcv.lastIdx {
		virtualPos += rcv.gridNum
	}
	//清空上一次统计到当前中间（可能）被跳过的数据
	for i := rcv.lastIdx + 1; i <= virtualPos; i++ {
		actualPos := i % rcv.gridNum
		rcv.sum -= rcv.dataGrid[actualPos]
		rcv.dataGrid[actualPos] = 0
	}
	rcv.dataGrid[idx] += val
	rcv.sum += val
	if val > rcv.max {
		rcv.max = val
	}
	if val < rcv.min {
		rcv.min = val
	}
	rcv.lastIdx = idx
	rcv.avg = rcv.calcAvg()
	rcv.lastStatTime = now
	return
}

func (rcv *PeriodicStatistic) calcAvg() int64 {
	//计算平均值时，去掉未写完的格子
	return (rcv.sum - rcv.dataGrid[rcv.lastIdx]) / (rcv.gridNum - 1)
}

func (rcv *PeriodicStatistic) printStats() {
	fmt.Println("dataGrid:s%, sum:d%, avg:d%", rcv.dataGrid, rcv.sum, rcv.avg)
}

// Avg 统计平均值
func (rcv *PeriodicStatistic) Avg() int64 {
	if rcv.expired() {
		return 0
	}
	return rcv.avg
}

// Max 统计最大值
func (rcv *PeriodicStatistic) Max() int64 {
	if rcv.expired() {
		return 0
	}
	return rcv.max
}

// Min 统计最小值
func (rcv *PeriodicStatistic) Min() int64 {
	if rcv.expired() {
		return 0
	}
	return rcv.min
}

// Sum 统计总数
func (rcv *PeriodicStatistic) Sum() int64 {
	if rcv.expired() {
		return 0
	}
	return rcv.sum
}
