package flv

import "github.com/bugVanisher/streamer/statistics"

type Hook interface {
	OnStatisticStat(statistics.StreamHandler)
}
