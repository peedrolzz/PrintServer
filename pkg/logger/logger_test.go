package logger

import (
	"log/slog"
	"strings"
	"testing"
)

func TestSanitizeRemovesCRLF(t *testing.T) {
	in := "ok\n[FAKE] injected\r\nmore"
	out := sanitize(in)
	if strings.ContainsAny(out, "\n\r") {
		t.Fatalf("control chars not stripped: %q", out)
	}
}

func TestSanitizeTruncates(t *testing.T) {
	long := strings.Repeat("A", 5000)
	out := sanitize(long)
	if len(out) > 4150 {
		t.Fatalf("too long: %d", len(out))
	}
	if !strings.HasSuffix(out, "(truncated)") {
		t.Fatalf("missing truncation marker: %q", out[len(out)-20:])
	}
}

func TestHashedSerialDeterministic(t *testing.T) {
	a := HashedSerial("ABC123")
	b := HashedSerial("ABC123")
	if a.Value.String() != b.Value.String() {
		t.Fatal("hash not deterministic")
	}
	c := HashedSerial("ABC124")
	if a.Value.String() == c.Value.String() {
		t.Fatal("different serials collided")
	}
}

func TestSafeStringProducesSlogAttr(t *testing.T) {
	attr := SafeString("ssid", "x\ny")
	if attr.Key != "ssid" {
		t.Fatalf("key wrong: %s", attr.Key)
	}
	if strings.Contains(attr.Value.String(), "\n") {
		t.Fatalf("not sanitized: %q", attr.Value.String())
	}
	_ = slog.Default
}
