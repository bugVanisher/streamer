package statistics

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
)

//ProcStat process stat from /proc/[pid]/stat
type ProcStat struct {
	// The process ID.
	PID int
	// The filename of the executable.
	Comm string
	// The process state.
	State string
	// The PID of the parent of this process.
	PPID int
	// The process group ID of the process.
	PGRP int
	// The session ID of the process.
	Session int
	// The controlling terminal of the process.
	TTY int
	// The ID of the foreground process group of the controlling terminal of
	// the process.
	TPGID int
	// The kernel flags word of the process.
	Flags uint
	// The number of minor faults the process has made which have not required
	// loading a memory page from disk.
	MinFlt uint
	// The number of minor faults that the process's waited-for children have
	// made.
	CMinFlt uint
	// The number of major faults the process has made which have required
	// loading a memory page from disk.
	MajFlt uint
	// The number of major faults that the process's waited-for children have
	// made.
	CMajFlt uint
	// Amount of time that this process has been scheduled in user mode,
	// measured in clock ticks.
	UTime uint
	// Amount of time that this process has been scheduled in kernel mode,
	// measured in clock ticks.
	STime uint
	// Amount of time that this process's waited-for children have been
	// scheduled in user mode, measured in clock ticks.
	CUTime uint
	// Amount of time that this process's waited-for children have been
	// scheduled in kernel mode, measured in clock ticks.
	CSTime uint
	// For processes running a real-time scheduling policy, this is the negated
	// scheduling priority, minus one.
	Priority int
	// The nice value, a value in the range 19 (low priority) to -20 (high
	// priority).
	Nice int
	// Number of threads in this process.
	NumThreads int
	// The time the process started after system boot, the value is expressed
	// in clock ticks.
	Starttime uint64
	// Virtual memory size in bytes.
	VSize uint64
	// Resident set size in pages.
	RSS uint64
}

// VirtualMemory returns the virtual memory size in bytes.
func (s ProcStat) VirtualMemory() uint64 {
	return s.VSize
}

// ResidentMemory returns the resident memory size in bytes.
func (s ProcStat) ResidentMemory() uint64 {
	return s.RSS * uint64(os.Getpagesize())
}

// CurrentProcStat 当前进程的stat数据
func CurrentProcStat() (ProcStat, error) {
	pid := os.Getpid()
	statfile := "/proc/" + strconv.Itoa(pid) + "/stat"
	return NewProcStat(statfile, pid)
}

// NewProcStat 生成一个进程的stat数据
func NewProcStat(statfile string, pid int) (ProcStat, error) {
	f, err := os.Open(statfile)
	if err != nil {
		return ProcStat{}, err
	}
	defer f.Close()

	data, err := ioutil.ReadAll(f)
	if err != nil {
		return ProcStat{}, err
	}

	var (
		ignore int

		s = ProcStat{PID: pid}
		l = bytes.Index(data, []byte("("))
		r = bytes.LastIndex(data, []byte(")"))
	)

	if l < 0 || r < 0 {
		return ProcStat{}, fmt.Errorf(
			"unexpected format, couldn't extract comm: %s",
			data,
		)
	}

	s.Comm = string(data[l+1 : r])
	_, err = fmt.Fscan(
		bytes.NewBuffer(data[r+2:]),
		&s.State,
		&s.PPID,
		&s.PGRP,
		&s.Session,
		&s.TTY,
		&s.TPGID,
		&s.Flags,
		&s.MinFlt,
		&s.CMinFlt,
		&s.MajFlt,
		&s.CMajFlt,
		&s.UTime,
		&s.STime,
		&s.CUTime,
		&s.CSTime,
		&s.Priority,
		&s.Nice,
		&s.NumThreads,
		&ignore,
		&s.Starttime,
		&s.VSize,
		&s.RSS,
	)
	if err != nil {
		return ProcStat{}, err
	}

	return s, nil
}
