//go:build windows

package tray

import "testing"

// ModelGroup-like sanity checks for the small pure helpers the tray relies on.

func TestTruncateUTF16(t *testing.T) {
	cases := []struct {
		in   string
		max  int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10c", 10, "exactly10c"},
		{"abcdefghijklm", 10, "abcdefghij"},
		{"中文标题很长的时候要截断", 4, "中文标题"},
		{"", 5, ""},
	}
	for _, tc := range cases {
		if got := truncateUTF16(tc.in, tc.max); got != tc.want {
			t.Errorf("truncateUTF16(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}

func TestUTF16FromStringIsNulTerminated(t *testing.T) {
	got := utf16FromString("abc")
	if len(got) != 4 {
		t.Fatalf("expected 4 uint16 (3 runes + NUL), got %d", len(got))
	}
	if got[3] != 0 {
		t.Fatalf("missing NUL terminator: %v", got)
	}
}

func TestIsSupportedOnWindows(t *testing.T) {
	if !IsSupported() {
		t.Fatal("IsSupported must be true on Windows")
	}
}

// ConfirmQuit must be what lets wndProc tear the window down; before it is
// called a cancel must leave the tray alive.
func TestConfirmQuitFlipsGuard(t *testing.T) {
	prev := gQuitConfirmed
	gQuitConfirmed = false
	defer func() { gQuitConfirmed = prev }()

	if gQuitConfirmed {
		t.Fatal("guard should start false")
	}
	ConfirmQuit()
	if !gQuitConfirmed {
		t.Fatal("ConfirmQuit did not flip the guard")
	}
}
