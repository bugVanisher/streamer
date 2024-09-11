package cmd

import (
	"github.com/bugVanisher/streamer/pusher"
	"github.com/spf13/cobra"
)

var upstream = &cobra.Command{
	Use:   "push",
	Short: "Streaming upstream",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		rtmpPusher := pusher.NewRtmpPusher(up.rUrl, up.sourceFile)
		return pusher.Launch("test", rtmpPusher, duration)
	},
}

type upstreamArgs struct {
	rUrl       string
	sourceFile string
}

var up upstreamArgs

func init() {
	rootCmd.AddCommand(upstream)

	upstream.Flags().StringVarP(&up.rUrl, "url", "u", "", "Upstream URL")
	upstream.MarkFlagRequired("url")
	upstream.Flags().StringVarP(&up.sourceFile, "file", "f", "", "File to upstream")
	upstream.MarkFlagRequired("file")
}
