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
		code := cli.ExitCodeForError(err)
		if jsonEvents {
			os.Exit(code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(code)
	}
}

func hasJSONEventsFlag(args []string) bool {
	return cli.HasJSONEventsArg(args)
}
