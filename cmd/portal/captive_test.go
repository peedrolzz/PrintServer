package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCaptivePortalUsesRequestHost(t *testing.T) {
	h := captivePortalHandler()
	for _, tc := range []struct {
		name, host, want string
	}{
		{"hotspot", "10.42.0.1", "https://10.42.0.1/"},
		{"cliente", "172.16.113.75", "https://172.16.113.75/"},
		{"mdns", "printserver.local", "https://printserver.local/"},
		{"host invalido", "evil.example/aside", "https://printserver.local/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://printserver.local/", nil)
			r.Host = tc.host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != http.StatusOK || !strings.Contains(body, tc.want) {
				t.Fatalf("code=%d body=%q, queria 200 com %q", w.Code, body, tc.want)
			}
		})
	}
}

func TestCaptivePortalOtherPathsRedirect(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://172.16.113.75/wifi", nil)
	w := httptest.NewRecorder()
	captivePortalHandler().ServeHTTP(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "https://172.16.113.75/" {
		t.Fatalf("code=%d location=%q", w.Code, w.Header().Get("Location"))
	}
}
