package hls

import (
	"fmt"
	"testing"
)

func TestTSCache_GetItem(t *testing.T) {
	data := make([]byte, 1<<20)
	tsCache := NewTSCache("test1", "./", 15000)
	for i := 0; i < 100; i++ {
		tsFileName := fmt.Sprintf("test1-%d.ts", i)
		tsItem := TSItem{
			Name:     tsFileName,
			SeqNum:   i,
			Duration: ((i % 4) + 1) * 1000,
			Data:     data,
		}
		tsCache.SetItem(tsFileName, tsItem)
	}

	m3u8, err := tsCache.GetM3U8PlayList()
	if err != nil {
		fmt.Println("gen m3u8 fail", err.Error())
		return
	}
	fmt.Println("m3u8:", string(m3u8))
	for i := 80; i < 100; i++ {
		tsFileName := fmt.Sprintf("test1-%d.ts", i)
		item, err := tsCache.GetItem(tsFileName)
		if err != nil {
			fmt.Printf("%s, not exist\n", tsFileName)
			continue
		}
		fmt.Printf("name:%s, seq:%d, duration:%d in cache\n", tsFileName, item.SeqNum, item.Duration)
	}
}

//BenchmarkTSCache_GetItem-16    	20000000	       106 ns/op
func BenchmarkTSCache_GetItem(b *testing.B) {
	data := make([]byte, 1<<20)
	tsCache := NewTSCache("test1", "./", 15000)
	for i := 0; i < 10; i++ {
		tsFileName := fmt.Sprintf("test1-%d.ts", i)
		tsItem := TSItem{
			Name:     tsFileName,
			SeqNum:   i,
			Duration: 3000,
			Data:     data,
		}
		tsCache.SetItem(tsFileName, tsItem)
	}

	for i := 0; i < b.N; i++ {
		tsCache.GetM3U8PlayList()
		tsCache.GetItem("test1-5.ts")
	}
}
