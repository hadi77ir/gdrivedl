package main

import (
	"fmt"
	"os"

	"github.com/hadi77ir/gdrivedl"
)

func main() {
	if err := gdrivedl.RunCLI(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
