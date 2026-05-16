package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
)

func TestExitCodeForError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "success", err: nil, want: ExitSuccess},
		{name: "cancelled", err: context.Canceled, want: ExitCancelled},
		{name: "invalid args", err: errors.New("invalid URL: \"bad\""), want: ExitInvalidArguments},
		{name: "server behavior", err: errors.New("server ignored Range header for x"), want: ExitServerBehaviorError},
		{name: "network", err: &net.DNSError{Err: "no such host", Name: "bad.example"}, want: ExitNetworkFailure},
		{name: "disk", err: &os.PathError{Op: "open", Path: "x", Err: errors.New("access denied")}, want: ExitDiskFailure},
		{name: "manifest", err: errors.New("manifest not found: file.quickget.json"), want: ExitManifestResumeError},
		{name: "verification", err: errors.New("hash mismatch"), want: ExitVerificationMismatch},
		{name: "fallback", err: errors.New("boom"), want: ExitGeneralRuntimeError},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ExitCodeForError(tc.err)
			if got != tc.want {
				t.Fatalf("ExitCodeForError() = %d, want %d", got, tc.want)
			}
		})
	}
}

