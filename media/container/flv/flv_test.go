package flv

import (
	"io"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDemuxer(t *testing.T) {
	f, err := os.Open("test.flv")
	require.Nil(t, err)
	d := NewDemuxer(f)
	headers, err := d.Streams()
	require.Nil(t, err)
	require.Equal(t, 2, len(headers))
	t.Logf("headers.len:%d\n", len(headers))
	count := 0
	for {
		pkt, err := d.ReadPacket()
		if err != nil {
			if err == io.EOF {
				t.Logf("read packet end, count:%d\n", count)
				break
			}
		}
		t.Logf("pkt.Time:%d\n", pkt.Time/time.Millisecond)
		count++
	}
	require.Equal(t, 14336, count)
}
