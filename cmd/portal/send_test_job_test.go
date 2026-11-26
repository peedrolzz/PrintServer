package main

import (
	"bytes"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/printserver/printserver/pkg/bridgeack"
	"github.com/printserver/printserver/pkg/config"
)

func fakeBridge(t *testing.T, ack byte) (int, <-chan []byte) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	received := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			received <- nil
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		data, _ := io.ReadAll(conn)
		received <- data
		_, _ = conn.Write([]byte{ack})
	}()

	return ln.Addr().(*net.TCPAddr).Port, received
}

func testApp(t *testing.T) *app {
	t.Helper()
	tokenPath := filepath.Join(t.TempDir(), "bridge-token")
	if err := os.WriteFile(tokenPath, []byte(strings.Repeat("a", 32)), 0o600); err != nil {
		t.Fatal(err)
	}
	return &app{bridgeTokPath: tokenPath}
}

func TestSendTestJobWaitsForConfirmation(t *testing.T) {
	port, received := fakeBridge(t, bridgeack.OK)
	a := testApp(t)
	p := config.Printer{Name: "Impressora de teste", TCPPort: port, Enabled: true}

	if err := a.sendTestJob(p); err != nil {
		t.Fatalf("teste de impressão falhou: %v", err)
	}
	data := <-received
	if len(data) == 0 {
		t.Fatal("bridge não recebeu dados")
	}
	if !bytes.Contains(data, []byte{0x0C, 0x1B, 'E'}) {
		t.Error("payload não ejeta a página (form feed + reset PCL)")
	}
	if bytes.Contains(data, []byte{0x1D, 'V', 0x00}) {
		t.Error("payload ainda usa comando de gaveta ESC/POS")
	}
}

func TestSendTestJobReportsBridgeFailure(t *testing.T) {
	port, _ := fakeBridge(t, bridgeack.Failure)
	a := testApp(t)
	p := config.Printer{Name: "Impressora de teste", TCPPort: port, Enabled: true}

	if err := a.sendTestJob(p); err == nil {
		t.Fatal("bridge com falha deveria devolver erro ao painel")
	}
}

func TestBridgeErrorMessage(t *testing.T) {
	tests := map[byte]string{
		bridgeack.NoDevice:   "dispositivo USB",
		bridgeack.Permission: "permissão",
		bridgeack.QueueFull:  "ocupada",
		bridgeack.Timeout:    "tempo",
	}
	for code, want := range tests {
		if got := bridgeack.Message(code); !strings.Contains(got, want) {
			t.Errorf("código %d: mensagem %q não contém %q", code, got, want)
		}
	}
}
