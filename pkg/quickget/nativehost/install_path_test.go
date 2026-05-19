package nativehost

import "testing"

func TestNormalizeWindowsExecutablePath(t *testing.T) {
	t.Run("device path prefix removed", func(t *testing.T) {
		in := `\\?\C:\Dev\Projects\QuickGet\quickget-native-host.exe`
		got := normalizeWindowsExecutablePath(in)
		want := `C:\Dev\Projects\QuickGet\quickget-native-host.exe`
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("unc path prefix normalized", func(t *testing.T) {
		in := `\\?\UNC\server\share\quickget-native-host.exe`
		got := normalizeWindowsExecutablePath(in)
		want := `\\server\share\quickget-native-host.exe`
		if got != want {
			t.Fatalf("expected %q, got %q", want, got)
		}
	})

	t.Run("plain path unchanged", func(t *testing.T) {
		in := `C:\Dev\Projects\QuickGet\quickget-native-host.exe`
		got := normalizeWindowsExecutablePath(in)
		if got != in {
			t.Fatalf("expected %q, got %q", in, got)
		}
	})
}
