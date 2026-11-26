package main

import (
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfigErrorShowsClearMessage(t *testing.T) {
	a := &app{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	rec := httptest.NewRecorder()
	a.configError(rec, errors.New("arquivo ausente"))

	body := rec.Body.String()
	if !strings.Contains(body, "Configuração incompleta") {
		t.Fatalf("missing clear title: %s", body)
	}
	if !strings.Contains(body, "Reinicie o aparelho") {
		t.Fatalf("missing recovery hint: %s", body)
	}
	if strings.Contains(body, ">config<") {
		t.Fatalf("still shows the raw config error: %s", body)
	}
}
