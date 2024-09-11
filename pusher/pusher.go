package pusher

import "context"

type Pusher interface {
	Publish(ctx context.Context) error
}
