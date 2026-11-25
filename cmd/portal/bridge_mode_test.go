package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBridgeOpenFrom(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "bridge-token-mode")
	if !bridgeOpenFrom(marker) {
		t.Fatal("sem marcador a impressão deve ser aberta")
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if bridgeOpenFrom(marker) {
		t.Fatal("com marcador a impressão deve ficar protegida")
	}
}
