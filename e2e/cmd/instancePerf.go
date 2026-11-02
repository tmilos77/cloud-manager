package main

import (
	"github.com/spf13/cobra"
)

var cmdInstancePerf = &cobra.Command{
	Use: "perf",
}

func init() {
	cmdInstance.AddCommand(cmdInstancePerf)
}
