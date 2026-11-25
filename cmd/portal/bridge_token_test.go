package main

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/printserver/printserver/pkg/logger"
)

func TestReadBridgeToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bridge-token")
	if err := os.WriteFile(path, []byte("abc123def456\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a := &app{bridgeTokPath: path, log: logger.Default()}
	if got := a.readBridgeToken(); got != "abc123def456" {
		t.Fatalf("token lido = %q, esperado %q", got, "abc123def456")
	}

	a.bridgeTokPath = filepath.Join(t.TempDir(), "ausente")
	if got := a.readBridgeToken(); got != "" {
		t.Fatalf("token ausente = %q, esperado vazio", got)
	}
}

func TestBridgePageShowsToken(t *testing.T) {
	tmpl, err := template.ParseFS(tplFS, "templates/base.html", "templates/bridge.html")
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	data := pageData{CSRF: "c", BridgeOpen: true, BridgeToken: "abc123def456"}
	if err := tmpl.ExecuteTemplate(&out, "content", data); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{"abc123def456", "readonly", "Copiar", "Só quem tiver o token imprime"} {
		if !strings.Contains(body, want) {
			t.Errorf("falta %q na tela de Impressão", want)
		}
	}
}
