package main

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteHotspotPassword(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hotspot-password")
	if err := os.WriteFile(path, []byte("printserver\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{hotspotPasswordPath: path}
	if err := a.writeHotspotPassword("senha-nova-123"); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "senha-nova-123\n" {
		t.Fatalf("password file = %q", got)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", st.Mode().Perm())
	}
	if err := a.writeHotspotPassword("com\nquebra"); err == nil {
		t.Fatal("newline must be rejected")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
}

func TestSafeReturnHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"172.16.113.75", "172.16.113.75"},
		{"printserver-f39f.local", "printserver-f39f.local"},
		{"10.42.0.1:443", "10.42.0.1:443"},
		{"", "printserver.local"},
		{"evil.com/x", "printserver.local"},
		{"user@evil.com", "printserver.local"},
	} {
		r := httptest.NewRequest("GET", "/wifi", nil)
		r.Host = tc.in
		if got := safeReturnHost(r); got != tc.want {
			t.Errorf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
}
