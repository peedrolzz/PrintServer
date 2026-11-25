package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCaptivePortalUsesStableHost(t *testing.T) {
	h := captivePortalHandler("printserver-f39f.local")
	for _, tc := range []struct {
		name, host string
	}{
		{"sonda da apple", "captive.apple.com"},
		{"hotspot", "10.42.0.1"},
		{"cliente", "172.16.113.75"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "http://"+tc.host+"/hotspot-detect.html", nil)
			r.Host = tc.host
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			body := w.Body.String()
			want := "https://printserver-f39f.local/"
			if w.Code != http.StatusOK || !strings.Contains(body, want) ||
				strings.Contains(body, tc.host) {
				t.Fatalf("code=%d body=%q, queria 200 com %q", w.Code, body, want)
			}
		})
	}
}

func TestCaptivePortalOtherPathsRedirect(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "http://172.16.113.75/wifi", nil)
	w := httptest.NewRecorder()
	captivePortalHandler("printserver-f39f.local").ServeHTTP(w, r)
	if w.Code != http.StatusFound ||
		w.Header().Get("Location") != "https://printserver-f39f.local/" {
		t.Fatalf("code=%d location=%q", w.Code, w.Header().Get("Location"))
	}
}
