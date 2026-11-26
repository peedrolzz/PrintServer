package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"time"
)

const SessionCookie = "printserver_session"

type Session struct {
	IssuedAt int64 `json:"iat"`
	MaxAge   int64 `json:"max_age"`
}

type Manager struct {
	key []byte
	TTL time.Duration
}

func NewManager(key []byte, ttl time.Duration) *Manager {
	return &Manager{key: key, TTL: ttl}
}

func (m *Manager) Issue(w http.ResponseWriter) error {
	s := Session{IssuedAt: time.Now().Unix(), MaxAge: int64(m.TTL.Seconds())}
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	payload := base64.RawURLEncoding.EncodeToString(b)
	sig := m.sign([]byte(payload))
	cookieValue := payload + "." + sig
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(m.TTL.Seconds()),
	})
	return nil
}

func (m *Manager) Verify(r *http.Request) error {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return ErrNoSession
	}
	payload, sig := SplitFirst(c.Value, ".")
	if sig == "" {
		return ErrBadSession
	}
	expected := m.sign([]byte(payload))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return ErrBadSession
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return ErrBadSession
	}
	var s Session
	if err := json.Unmarshal(raw, &s); err != nil {
		return ErrBadSession
	}
	if time.Since(time.Unix(s.IssuedAt, 0)) > time.Duration(s.MaxAge)*time.Second {
		return ErrBadSession
	}
	return nil
}

// RotateKey invalida todos os cookies já emitidos. Usar no logout e na
// troca de senha: apagar apenas o cookie do navegador não revoga uma cópia
// que tenha sido roubada.
func (m *Manager) RotateKey() error {
	key := make([]byte, len(m.key))
	if _, err := rand.Read(key); err != nil {
		return err
	}
	m.key = key
	return nil
}

func (m *Manager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

func (m *Manager) sign(payload []byte) string {
	h := hmac.New(sha256.New, m.key)
	h.Write(payload)
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

var ErrNoSession = errors.New("session: missing")

var ErrBadSession = errors.New("session: invalid or expired")

func (m *Manager) RequireSession(login string, whitelist []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, p := range whitelist {
			if r.URL.Path == p {
				next.ServeHTTP(w, r)
				return
			}
		}
		if err := m.Verify(r); err != nil {
			http.Redirect(w, r, login, http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}
