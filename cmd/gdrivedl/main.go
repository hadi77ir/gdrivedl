package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/hadi77ir/gdrivedl"
)

func main() {
	if err := gdrivedl.RunCLI(os.Args); err != nil {
		if errors.Is(err, context.Canceled) {
			os.Exit(130)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
