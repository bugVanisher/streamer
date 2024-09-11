package rtmp

import (
	"github.com/bugVanisher/streamer/media/container/flv/flvio"
)

var (
	AMFMapOnStatusPublishStart = flvio.AMFMap{
		"level":       "status",
		"code":        "NetStream.Publish.Start",
		"description": "Start publishing",
	}
	AMFMapOnStatusPublishBadName = flvio.AMFMap{
		"level":       "status",
		"code":        "NetStream.Publish.BadName",
		"description": "Failed publishing",
	}
	AMFMapOnStatusPublishStreamDuplicated = flvio.AMFMap{
		"level":       "status",
		"code":        "NetStream.Publish.StreamDuplicated",
		"description": "Stream duplicated",
	}
)

var (
	codeAMFMap = map[string]flvio.AMFMap{
		"NetStream.Publish.Start":            AMFMapOnStatusPublishStart,
		"NetStream.Publish.BadName":          AMFMapOnStatusPublishBadName,
		"NetStream.Publish.StreamDuplicated": AMFMapOnStatusPublishStreamDuplicated,
	}
)
