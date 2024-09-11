package cmd

import (
	"github.com/bugVanisher/streamer/downstream"
	"github.com/spf13/cobra"
	"io"
	"os"
)

var downstreamCmd = &cobra.Command{
	Use:   "pull",
	Short: "Streaming downstream",
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		var writer io.Writer
		if down.outFile != "" {
			file, err := os.OpenFile(down.outFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
			if err != nil {
				return err
			}
			defer func(file *os.File) {
				err := file.Close()
				if err != nil {

				}
			}(file)
			writer = io.Writer(file)
		} else {
			writer = io.Discard
		}
		down := downstream.NewFlvDownStreamer(down.pUrl, writer)

		return downstream.Launch("download", down, duration)
	},
}

type downstreamArgs struct {
	pUrl    string
	outFile string
}

var down downstreamArgs

func init() {
	rootCmd.AddCommand(downstreamCmd)

	downstreamCmd.Flags().StringVarP(&down.pUrl, "url", "u", "", "Downstream URL")
	downstreamCmd.MarkFlagRequired("url")
	downstreamCmd.Flags().StringVarP(&down.outFile, "file", "f", "", "File to save")
}
