package auth

import (
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLimiterBlocksAfterMax(t *testing.T) {
	l := NewLimiter(time.Second, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("x") {
			t.Fatalf("hit %d should allow", i)
		}
	}
	if l.Allow("x") {
		t.Fatalf("4th should block")
	}
}

func TestLimiterIsPerKey(t *testing.T) {
	l := NewLimiter(time.Second, 1)
	if !l.Allow("a") || !l.Allow("b") {
		t.Fatal("different keys share bucket")
	}
	if l.Allow("a") {
		t.Fatal("a should be blocked")
	}
}

func TestLimiterResetsAfterWindow(t *testing.T) {
	l := NewLimiter(50*time.Millisecond, 1)
	if !l.Allow("x") {
		t.Fatal()
	}
	if l.Allow("x") {
		t.Fatal()
	}
	time.Sleep(80 * time.Millisecond)
	if !l.Allow("x") {
		t.Fatal("should allow after window")
	}
}

func TestHTTPKeyIPv6(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://example.local/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = "[::1]:54321"
	if got := HTTPKey(req); got != "::1" {
		t.Fatalf("HTTPKey = %q, want ::1", got)
	}
	req.RemoteAddr = "192.0.2.10:1234"
	if got := HTTPKey(req); got != "192.0.2.10" {
		t.Fatalf("HTTPKey = %q, want 192.0.2.10", got)
	}
}

func TestPeerKeyIgnoraPortaNoIPv6(t *testing.T) {
	addr, err := net.ResolveTCPAddr("tcp", "[::1]:54321")
	if err != nil {
		t.Fatal(err)
	}
	if got := PeerKey(addr); got != "::1" {
		t.Fatalf("PeerKey = %q, want ::1", got)
	}
}

func TestLimiterDropsIdleKeys(t *testing.T) {
	l := NewLimiter(20*time.Millisecond, 1)
	l.Allow("idle")
	time.Sleep(40 * time.Millisecond)
	// Nova chamada com a mesma chave: ocorrências antigas fora da janela
	// não podem ficar no mapa para sempre.
	l.Allow("idle")
	l.mu.Lock()
	stored := len(l.hits["idle"])
	l.mu.Unlock()
	if stored != 1 {
		t.Fatalf("idle key kept %d timestamps, want 1", stored)
	}
}
