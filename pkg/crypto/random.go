package crypto

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/hex"
	"io"
)

func Bytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return nil, err
	}
	return b, nil
}

func Hex(n int) (string, error) {
	b, err := Bytes(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func Base32(nChars int) (string, error) {
	nBytes := (nChars*5 + 7) / 8
	b, err := Bytes(nBytes)
	if err != nil {
		return "", err
	}
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	if len(s) > nChars {
		s = s[:nChars]
	}
	return s, nil
}

func HotspotPassword() (string, error) {
	const alpha = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	const n = 8
	out := make([]byte, 0, n)
	buf := make([]byte, 1)
	limitInt := (256 / len(alpha)) * len(alpha)
	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		if int(buf[0]) >= limitInt {
			continue
		}
		out = append(out, alpha[int(buf[0])%len(alpha)])
	}
	return string(out), nil
}
