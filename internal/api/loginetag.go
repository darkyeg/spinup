package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func (l Logins) ETag() string {
	data, err := json.Marshal(l)
	if err != nil {
		return ""
	}
	hash := sha256.Sum256(data)
	return `"` + hex.EncodeToString(hash[:]) + `"`
}
