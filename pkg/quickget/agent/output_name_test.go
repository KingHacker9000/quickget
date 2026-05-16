package agent

import "testing"

func TestDeriveSafeOutputFilenameFromURL(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		want   string
	}{
		{name: "simple basename", rawURL: "https://getsamplefiles.com/download/zip/sample-1.zip", want: "sample-1.zip"},
		{name: "encoded basename", rawURL: "https://unit.test/files/report%20Q1.pdf", want: "report Q1.pdf"},
		{name: "path traversal chars sanitized", rawURL: "https://unit.test/files/%2e%2e%2fsecret.txt", want: "secret.txt"},
		{name: "empty path fallback", rawURL: "https://unit.test", want: defaultOutputFilename},
		{name: "invalid url fallback", rawURL: "://", want: defaultOutputFilename},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveSafeOutputFilenameFromURL(tc.rawURL)
			if got != tc.want {
				t.Fatalf("deriveSafeOutputFilenameFromURL(%q)=%q want %q", tc.rawURL, got, tc.want)
			}
		})
	}
}
