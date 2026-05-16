package cli

import "testing"

func TestParseByteSize(t *testing.T) {
	cases := map[string]int64{
		"1MB":   1024 * 1024,
		"256KB": 256 * 1024,
		"2GB":   2 * 1024 * 1024 * 1024,
	}
	for in, want := range cases {
		got, err := parseByteSize(in)
		if err != nil {
			t.Fatalf("parseByteSize(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("parseByteSize(%q)=%d want %d", in, got, want)
		}
	}
}

func TestParseHTTPModes(t *testing.T) {
	got, err := parseHTTPModes("auto,http1,auto")
	if err != nil {
		t.Fatalf("parseHTTPModes error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 modes, got %d", len(got))
	}
}
