package main

import (
	"fmt"
	"os"
	"path/filepath"

	"quickget/pkg/quickget/cli"
)

func main() {
	jsonEvents := hasJSONEventsFlag(os.Args[1:])
	if err := cli.Run(os.Args[1:], os.Stdout, os.Stderr, filepath.Base(os.Args[0])); err != nil {
		if jsonEvents {
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func hasJSONEventsFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-json-events" || arg == "--json-events" || arg == "-json-events=true" || arg == "--json-events=true" {
			return true
		}
	}
	return false
}
