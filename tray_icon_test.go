package main

import (
	"encoding/base64"
	"testing"
)

// The embedded icon must decode to a real ICO; a typo in the base64 literal
// would otherwise only show up as a blank tray icon at runtime.
func TestTrayIconDecodes(t *testing.T) {
	icon := trayIconICO
	if len(icon) == 0 {
		t.Fatal("tray icon did not decode to any bytes")
	}
	// ICO files start with a 0,0 reserved word followed by type 1 (icon).
	if len(icon) < 6 {
		t.Fatalf("icon too short: %d bytes", len(icon))
	}
	if icon[0] != 0 || icon[1] != 0 {
		t.Fatalf("not an ICO: reserved bytes = %v", icon[:2])
	}
	if icon[2] != 1 || icon[3] != 0 {
		t.Fatalf("not an ICO: type = %v", icon[2:4])
	}
	count := int(icon[4]) | int(icon[5])<<8
	if count == 0 {
		t.Fatal("ICO declares no images")
	}
	if count < 2 {
		t.Fatalf("expected several icon sizes, got %d", count)
	}
}

func TestDecodeEmbeddedBase64JoinsParts(t *testing.T) {
	// Split a known payload the way the generator does.
	want := []byte("show me the story")
	enc := base64.StdEncoding.EncodeToString(want)
	mid := len(enc) / 2
	got := decodeEmbeddedBase64(enc[:mid], enc[mid:])
	if string(got) != string(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDecodeEmbeddedBase64PanicsOnGarbage(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for invalid base64")
		}
	}()
	decodeEmbeddedBase64("!!!not base64!!!")
}
