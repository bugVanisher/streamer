package main

import (
	"github.com/bugVanisher/streamer/cmd"
	"github.com/rs/zerolog/log"
	"os"
	"runtime"
)

func main() {
	defer func() {
		if err := recover(); err != nil {
			// print panic trace
			const size = 64 << 10
			buf := make([]byte, size)
			buf = buf[:runtime.Stack(buf, false)]
			log.Error().Str("stack", string(buf)).Any("error", err).Msg("panic recover")
		}
	}()
	exitCode := cmd.Execute()
	os.Exit(exitCode)
}
