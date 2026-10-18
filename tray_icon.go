package main

import (
	"encoding/base64"
	"strings"
)

// trayIconBase64 is the raw base64 text of the embedded icon, split across Go
// string literals by the generator. Concatenate first, then decode.
func decodeEmbeddedBase64(parts ...string) []byte {
	raw := strings.TrimSpace(strings.ReplaceAll(strings.Join(parts, ""), "\n", ""))
	out, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		panic("tray icon base64 is invalid: " + err.Error())
	}
	return out
}
