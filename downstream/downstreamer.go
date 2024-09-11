package downstream

import (
	"context"
)

type DownStreamer interface {
	Pull(context.Context) (bool, error)
}
