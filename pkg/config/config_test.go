package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestManagerRequiresHTTPS(t *testing.T) {
	m := &Manager{BaseURL: "http://1.2.3.4/x", CAFingerprint: pad64('a')}
	if err := m.Validate(); err == nil {
		t.Fatal("http should be rejected (V14)")
	}
}

func TestManagerRequiresFingerprint(t *testing.T) {
	m := &Manager{BaseURL: "https://1.2.3.4/x"}
	if err := m.Validate(); err == nil {
		t.Fatal("missing fingerprint should be rejected")
	}
}

func TestManagerAcceptsValid(t *testing.T) {
	m := &Manager{BaseURL: "https://manager.example/agent/", CAFingerprint: pad64('a')}
	if err := m.Validate(); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if m.BaseURL != "https://manager.example/agent" {
		t.Fatalf("trailing slash not stripped: %q", m.BaseURL)
	}
}

func TestPrintersStrictSchema(t *testing.T) {
	tests := []struct {
		name string
		p    Printers
		ok   bool
	}{
		{
			name: "valid",
			p: Printers{Printers: []Printer{{
				Name: "Epson", SerialNumber: "SN12345", TCPPort: 9100, Enabled: true,
			}}},
			ok: true,
		},
		{
			name: "duplicate serial",
			p: Printers{Printers: []Printer{
				{Name: "A", SerialNumber: "X", TCPPort: 9100, Enabled: true},
				{Name: "B", SerialNumber: "X", TCPPort: 9101, Enabled: true},
			}},
			ok: false,
		},
		{
			name: "port out of range",
			p: Printers{Printers: []Printer{
				{Name: "A", SerialNumber: "X", TCPPort: 8080, Enabled: true},
			}},
			ok: false,
		},
		{
			name: "bad serial chars",
			p: Printers{Printers: []Printer{
				{Name: "A", SerialNumber: "bad serial /../", TCPPort: 9100, Enabled: true},
			}},
			ok: false,
		},
		{
			name: "null byte",
			p: Printers{Printers: []Printer{
				{Name: "A\x00B", SerialNumber: "X", TCPPort: 9100, Enabled: true},
			}},
			ok: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.p.Validate()
			if tt.ok && err != nil {
				t.Fatalf("expected ok, got %v", err)
			}
			if !tt.ok && err == nil {
				t.Fatalf("expected error")
			}
		})
	}
}

func TestPrintersSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "printers.json")
	p := &Printers{Printers: []Printer{{
		Name: "Epson", SerialNumber: "SN1", TCPPort: 9100, Enabled: true,
	}}}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode=%v want 0644", info.Mode().Perm())
	}
	got, err := LoadPrinters(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Printers) != 1 || got.Printers[0].SerialNumber != "SN1" {
		t.Fatalf("roundtrip failed: %+v", got)
	}
	if got.Printers[0].DevicePath != "/dev/printserver/by-serial/SN1" {
		t.Fatalf("device_path not normalized: %q", got.Printers[0].DevicePath)
	}
}

func pad64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
