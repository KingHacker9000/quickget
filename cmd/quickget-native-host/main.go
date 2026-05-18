package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"quickget/pkg/quickget/nativehost"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install-chrome":
			if err := runInstallChrome(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			return
		case "uninstall-chrome":
			if err := nativehost.UninstallChrome(); err != nil {
				fmt.Fprintln(os.Stderr, err.Error())
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "Chrome native messaging host uninstalled.")
			return
		}
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	host := nativehost.NewHost(os.Stdin, os.Stdout, os.Stderr)
	if err := host.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func runInstallChrome(args []string) error {
	fs := flag.NewFlagSet("install-chrome", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	hostPath := fs.String("path", "", "path to quickget-native-host executable")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := *hostPath
	if path == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		path = exe
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	result, err := nativehost.InstallChrome(path)
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "Chrome host manifest written: %s\n", result.ManifestPath)
	if result.RegistryConfigured {
		fmt.Fprintln(os.Stderr, "Chrome registry key registered.")
	} else {
		fmt.Fprintln(os.Stderr, "Registry registration failed; manual registry setup required.")
		fmt.Fprintln(os.Stderr, nativehost.InstallHelp(path))
	}
	return nil
}
