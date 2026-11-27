package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/printserver/printserver/pkg/auth"
	"github.com/printserver/printserver/pkg/bridgeack"
	"github.com/printserver/printserver/pkg/config"
	"github.com/printserver/printserver/pkg/printer"
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

// Um sistema comum só abre a porta 9100 e escreve o trabalho. Este teste
// percorre exatamente esse caminho, sem handshake, até o arquivo USB.
func TestHandleModoAbertoEncaminhaJob(t *testing.T) {
	devPath := filepath.Join(t.TempDir(), "lp0")
	if err := os.WriteFile(devPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	p := config.Printer{
		Name:         "Impressora de teste",
		SerialNumber: "SN1",
		TCPPort:      9100,
		Enabled:      true,
	}
	dev := printer.New(devPath, 4)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	limiter := auth.NewLimiter(time.Minute, maxPerIPPerMin)

	served := make(chan struct{})
	go func() {
		defer close(served)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		handle(log, conn, dev, p, nil, false, limiter)
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	job := []byte("\x1bE*** Cupom de teste ***\nTotal: 10,00\n\n\x0c\x1bE")
	if _, err := client.Write(job); err != nil {
		t.Fatal(err)
	}
	if err := client.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}

	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ack [1]byte
	if _, err := io.ReadFull(client, ack[:]); err != nil {
		t.Fatalf("sem confirmação do bridge: %v", err)
	}
	if ack[0] != bridgeack.OK {
		t.Fatalf("confirmação = %d, esperado %d", ack[0], bridgeack.OK)
	}

	select {
	case <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("handle não terminou")
	}

	got, err := os.ReadFile(devPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(job) {
		t.Fatalf("bytes no USB = %q, esperado %q", got, job)
	}
}

func TestMaxPerIPPerMinFromEnv(t *testing.T) {
	t.Setenv("PRINTBRIDGE_MAX_PER_MIN", "120")
	if got := maxPerIPPerMinFromEnv(); got != 120 {
		t.Fatalf("limite = %d, esperado 120", got)
	}

	t.Setenv("PRINTBRIDGE_MAX_PER_MIN", "999999")
	if got := maxPerIPPerMinFromEnv(); got != maxPerIPPerMinCap {
		t.Fatalf("limite não foi limitado: %d", got)
	}

	t.Setenv("PRINTBRIDGE_MAX_PER_MIN", "invalid")
	if got := maxPerIPPerMinFromEnv(); got != maxPerIPPerMin {
		t.Fatalf("valor inválido deveria usar o padrão, got %d", got)
	}
}

func TestMaxJobFromEnv(t *testing.T) {
	t.Setenv("PRINTBRIDGE_MAX_JOB", "1048576")
	if got := maxJobFromEnv(); got != 1048576 {
		t.Fatalf("limite = %d, esperado 1048576", got)
	}

	t.Setenv("PRINTBRIDGE_MAX_JOB", "9999999999")
	if got := maxJobFromEnv(); got != maxJobCap {
		t.Fatalf("limite não foi limitado: %d", got)
	}

	t.Setenv("PRINTBRIDGE_MAX_JOB", "invalid")
	if got := maxJobFromEnv(); got != maxJob {
		t.Fatalf("valor inválido deveria usar o padrão, got %d", got)
	}
}
