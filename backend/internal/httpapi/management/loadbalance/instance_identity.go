package loadbalance

import (
	"crypto/rand"
	"encoding/hex"
)

func randomInstanceID() string {
	buffer := make([]byte, 16)
	if _, err := rand.Read(buffer); err != nil {
		return "unknown-instance"
	}
	return hex.EncodeToString(buffer)
}
