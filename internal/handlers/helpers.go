package handlers

import (
	"crypto/rand"
	"encoding/hex"
)

// randID returns a short hex id used to make upload filenames unique.
func randID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	return hex.EncodeToString(b)
}
