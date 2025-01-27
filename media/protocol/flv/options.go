package flv

var DefaultOptions = NewOptions()

type Options struct {
	StatisticHook Hook
}

type Option func(*Options)

func NewOptions() Options {
	return Options{}
}

func WithStatHook(hook Hook) Option {
	return func(opts *Options) {
		opts.StatisticHook = hook
	}
}
