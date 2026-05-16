package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"quickget/pkg/quickget/agent"
	"quickget/pkg/quickget/store"
)

const (
	defaultTokenFileName = "agent-token"
	defaultShutdownGrace = 10 * time.Second
)

var (
	version     = "dev"
	buildCommit = ""
	buildDate   = ""
)

func main() {
	if len(os.Args) < 2 {
		printUsage(os.Stderr)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		if err := runServe(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "quickget-agent serve: %v\n", err)
			os.Exit(1)
		}
	case "token":
		if err := runToken(os.Args[2:]); err != nil {
			fmt.Fprintf(os.Stderr, "quickget-agent token: %v\n", err)
			os.Exit(1)
		}
	default:
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	addr := fs.String("addr", agent.DefaultAgentAddr, "listen address")
	statePath := fs.String("state", "", "custom state file path")
	tokenPath := fs.String("token", "", "custom token file path")
	verbose := fs.Bool("v", false, "verbose logging")

	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	if *verbose {
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	}

	tokPath, tok, err := loadOrCreateTokenAt(*tokenPath)
	if err != nil {
		return err
	}
	if *verbose {
		log.Printf("loaded auth token from %s", tokPath)
	}

	resolvedStatePath := *statePath
	if strings.TrimSpace(resolvedStatePath) == "" {
		resolvedStatePath, err = store.DefaultStatePath()
		if err != nil {
			return err
		}
	}
	st := store.JSONStore{Path: resolvedStatePath}
	mgr := agent.NewManager(st)
	if err := mgr.LoadState(); err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	srv := agent.NewServer(mgr, tok, version)
	srv.SetBuildInfo(buildCommit, buildDate)
	srv.SetAddr(*addr)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	fmt.Printf("QuickGet Agent listening on %s\n", srv.Addr())
	if *verbose {
		log.Printf("state file: %s", resolvedStatePath)
	}

	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	select {
	case err := <-errCh:
		return err
	case <-sigCtx.Done():
		if *verbose {
			log.Printf("shutdown signal received")
		}
	}

	pauseActiveDownloads(mgr, *verbose)
	if err := mgr.SaveState(); err != nil && *verbose {
		log.Printf("save state before shutdown failed: %v", err)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), defaultShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("shutdown server: %w", err)
	}

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	default:
	}

	if err := mgr.SaveState(); err != nil {
		return fmt.Errorf("save state: %w", err)
	}

	return nil
}

func runToken(args []string) error {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	tokenPath := fs.String("token", "", "custom token file path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}

	path, token, err := loadOrCreateTokenAt(*tokenPath)
	if err != nil {
		return err
	}

	fmt.Println("WARNING: Local development token. Do not share this token.")
	fmt.Printf("Token path: %s\n", path)
	fmt.Printf("Token: %s\n", token)
	return nil
}

func pauseActiveDownloads(mgr *agent.Manager, verbose bool) {
	for _, job := range mgr.List() {
		if job.Status != "running" {
			continue
		}
		if err := mgr.Pause(job.ID); err != nil {
			if verbose {
				log.Printf("pause %s failed: %v", job.ID, err)
			}
			_ = mgr.Cancel(job.ID)
		}
	}
}

func loadOrCreateTokenAt(customPath string) (string, string, error) {
	if strings.TrimSpace(customPath) == "" {
		token, err := agent.LoadOrCreateToken()
		if err != nil {
			return "", "", err
		}
		cfgDir, err := os.UserConfigDir()
		if err != nil {
			return "", "", err
		}
		path := filepath.Join(cfgDir, "QuickGet", defaultTokenFileName)
		return path, token, nil
	}

	path := filepath.Clean(customPath)
	if data, err := os.ReadFile(path); err == nil {
		token := strings.TrimSpace(string(data))
		if token == "" {
			return "", "", errors.New("agent token file is empty")
		}
		return path, token, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", "", err
	}
	token, err := generateToken()
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return "", "", err
	}
	return path, token, nil
}

func generateToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func printUsage(w *os.File) {
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  quickget-agent serve [options]")
	fmt.Fprintln(w, "  quickget-agent token [-token path]")
}
