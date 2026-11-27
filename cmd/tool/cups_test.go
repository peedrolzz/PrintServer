package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/printserver/printserver/pkg/config"
	"github.com/printserver/printserver/pkg/printprofile"
)

func TestDeviceURI(t *testing.T) {
	p := config.Printer{TCPPort: 9107}
	if got := deviceURI(p); got != "socket://127.0.0.1:9107" {
		t.Fatalf("URI inesperada: %q", got)
	}
}

func TestWriteProfile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printers.json")
	initial := []byte(`{"printers":[{"name":"Ricoh","serial_number":"ABC123","device_path":"/dev/printserver/by-serial/ABC123","tcp_port":9100,"enabled":true}],"config_revision":0}` + "\n")
	if err := os.WriteFile(path, initial, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeProfile(path, "ABC123", printprofile.PostScript); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadPrinters(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Printers[0].Profile != printprofile.PostScript {
		t.Fatalf("perfil não salvo: %q", cfg.Printers[0].Profile)
	}
	if err := writeProfile(path, "OUTRO", printprofile.PCL); err == nil {
		t.Fatal("serial inexistente deveria falhar")
	}
}
