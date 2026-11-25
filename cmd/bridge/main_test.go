package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBridgeTokenModeFrom(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "bridge-token-mode")
	if bridgeTokenModeFrom(marker) {
		t.Fatal("sem marcador o bridge deve aceitar qualquer cliente")
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if !bridgeTokenModeFrom(marker) {
		t.Fatal("com marcador o bridge deve exigir o token")
	}
}
