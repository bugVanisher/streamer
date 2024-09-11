package slice

import (
	"encoding/hex"
	"fmt"
	"github.com/bugVanisher/streamer/media/av"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestExtend(t *testing.T) {
	extend := NewExtend()
	extend[KSliceExtendKeyTimeStamp] = 1234
	extend[2] = 3456
	data := extend.Encode()
	fmt.Println(hex.EncodeToString(data))
	fmt.Printf("extend: %v\n", extend)

	extend2 := NewExtend()
	err := extend2.Decode(data)
	if err != nil {
		fmt.Printf("decode err %s\n", err.Error())
	}
	fmt.Printf("extend2: %v\n", extend2)
	require.Equal(t, extend, extend2)
}

func TestSlice(t *testing.T) {
	info := NewDataSliceInfo()
	data := make([]byte, 5003)
	var avPkt av.Packet
	avPkt.DataType = av.FLV_TAG_VIDEO
	avPkt.Time = time.Millisecond * 1324
	slicePkts := info.GenerateSlice(data, &avPkt)
	require.Equal(t, 4, len(slicePkts))
	for _, slicePkt := range slicePkts {
		fmt.Printf("gene: %#v\n", slicePkt)
		sliceHeader, _, err := ParseSliceHeader(slicePkt.Data)
		require.Equal(t, nil, err)
		sliceHeader.FrameDts = 1324
		sliceHeader.Data = slicePkt.Data
		fmt.Printf("pars: %#v\n", sliceHeader)
		require.Equal(t, slicePkt, sliceHeader)
	}
}
