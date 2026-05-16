package cli

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
)

const (
	ExitSuccess               = 0
	ExitGeneralRuntimeError   = 1
	ExitInvalidArguments      = 2
	ExitServerBehaviorError   = 3
	ExitNetworkFailure        = 4
	ExitDiskFailure           = 5
	ExitCancelled             = 6
	ExitManifestResumeError   = 7
	ExitVerificationMismatch  = 8
)

func ExitCodeForError(err error) int {
	if err == nil {
		return ExitSuccess
	}

	if errors.Is(err, context.Canceled) || strings.Contains(strings.ToLower(err.Error()), "download cancelled") {
		return ExitCancelled
	}

	if isInvalidArgumentError(err) {
		return ExitInvalidArguments
	}

	if isVerificationError(err) {
		return ExitVerificationMismatch
	}

	if isManifestError(err) {
		return ExitManifestResumeError
	}

	if isDiskError(err) {
		return ExitDiskFailure
	}

	if isServerBehaviorError(err) {
		return ExitServerBehaviorError
	}

	if isNetworkError(err) {
		return ExitNetworkFailure
	}

	return ExitGeneralRuntimeError
}

func isInvalidArgumentError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "invalid url") ||
		strings.Contains(msg, "requires exactly one") ||
		strings.Contains(msg, "exactly one positional") ||
		strings.Contains(msg, "flag -") ||
		strings.Contains(msg, "must be >") ||
		strings.Contains(msg, "must be >=") ||
		strings.Contains(msg, "missing url") ||
		strings.Contains(msg, "no command or url provided")
}

func isServerBehaviorError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "server ignored range header") ||
		strings.Contains(msg, "range handling may be unreliable") ||
		strings.Contains(msg, "try lowering -n") ||
		strings.Contains(msg, "unexpected success status")
}

func isNetworkError(err error) bool {
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "head request failed") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "no such host") ||
		strings.Contains(msg, "timeout")
}

func isDiskError(err error) bool {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "disk space check failed") ||
		strings.Contains(msg, "insufficient disk space") ||
		strings.Contains(msg, "no space left on device") ||
		strings.Contains(msg, "invalid write length")
}

func isManifestError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "manifest") ||
		strings.Contains(msg, ".quickget.json")
}

func isVerificationError(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "hash mismatch") ||
		strings.Contains(msg, "verification failed") ||
		strings.Contains(msg, "size mismatch") ||
		strings.Contains(msg, "file-size mismatch")
}
