package main

import (
	"fmt"
	"os"

	"github.com/NyaMisty/dap-cli-go/internal/cli"
)

func main() {
	if err := cli.NewDaemonCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
