package postgres

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/evilperson1337/requestarr/internal/requests"
)

type requestCursor struct {
	Version  int    `json:"v"`
	Snapshot string `json:"s"`
	Position int    `json:"p"`
}

// Signed cursors cannot be edited to jump around an existing snapshot. The
// signing key is persisted with that snapshot, not global process state.
func encodeRequestCursor(cursor requestCursor, key []byte) string {
	payload, _ := json.Marshal(cursor) // A fixed struct of integers and a UUID.
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func decodeRequestCursor(token string) (requestCursor, []byte, []byte, error) {
	var cursor requestCursor
	if len(token) > 512 {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[0])
	if err != nil {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(parts[1])
	if err != nil || len(signature) != sha256.Size {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&cursor) != nil || cursor.Version != 1 || cursor.Position < 1 || cursor.Position > 10000 {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	id := cursor.Snapshot
	if len(id) != 36 || id[8] != '-' || id[13] != '-' || id[18] != '-' || id[23] != '-' {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	if _, err := hex.DecodeString(strings.ReplaceAll(id, "-", "")); err != nil {
		return cursor, nil, nil, requests.ErrInvalidCursor
	}
	return cursor, payload, signature, nil
}

func validRequestCursorMAC(payload, signature, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(payload)
	return hmac.Equal(signature, mac.Sum(nil))
}
