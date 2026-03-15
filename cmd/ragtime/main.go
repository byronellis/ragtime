package main

import (
	"fmt"
	"os"

	"github.com/byronellis/ragtime/internal/cli"
)

func main() {
	if err := cli.NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
