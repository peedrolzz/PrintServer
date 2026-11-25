package auth

import (
	"crypto/hmac"
	"crypto/sha256"
)

func RawHMAC(key, msg []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return h.Sum(nil)
}

func VerifyRawHMAC(key, msg, mac []byte) bool {
	return hmac.Equal(RawHMAC(key, msg), mac)
}
