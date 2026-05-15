package main

import (
	"fmt"
	"os"
	"path/filepath"

	"quickget/pkg/quickget/cli"
)

func main() {
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr, filepath.Base(os.Args[0])); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
