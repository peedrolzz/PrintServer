package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/printserver/printserver/pkg/crypto"
)

const CSRFCookie = "printserver_csrf"

const CSRFHeader = "X-CSRF-Token"

const CSRFFormField = "_csrf"

func IssueCSRF(w http.ResponseWriter, r *http.Request) (string, error) {
	if c, err := r.Cookie(CSRFCookie); err == nil && validCSRFFormat(c.Value) {
		return c.Value, nil
	}
	tok, err := crypto.Hex(32)
	if err != nil {
		return "", err
	}
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookie,
		Value:    tok,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int((2 * time.Hour).Seconds()),
	})
	return tok, nil
}

func VerifyCSRF(r *http.Request) error {
	c, err := r.Cookie(CSRFCookie)
	if err != nil {
		return errors.New("csrf: cookie missing")
	}
	sent := r.Header.Get(CSRFHeader)
	if sent == "" {
		sent = r.FormValue(CSRFFormField)
	}
	if sent == "" {
		return errors.New("csrf: token missing")
	}
	if !hmac.Equal([]byte(c.Value), []byte(sent)) {
		return errors.New("csrf: token mismatch")
	}
	return nil
}

func validCSRFFormat(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

func SignHMAC(key, msg []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write(msg)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func VerifyHMAC(key, msg []byte, expected string) bool {
	actual := SignHMAC(key, msg)
	return hmac.Equal([]byte(actual), []byte(expected))
}

func SplitFirst(s, sep string) (string, string) {
	idx := strings.Index(s, sep)
	if idx == -1 {
		return s, ""
	}
	return s[:idx], s[idx+len(sep):]
}
