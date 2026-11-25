package auth

import (
	"bytes"
	"testing"
)

func TestRawHMACDeterministic(t *testing.T) {
	k := []byte("secret")
	m := []byte("message")
	a := RawHMAC(k, m)
	b := RawHMAC(k, m)
	if !bytes.Equal(a, b) {
		t.Fatal("hmac not deterministic")
	}
	if len(a) != 32 {
		t.Fatalf("len=%d", len(a))
	}
}

func TestVerifyRawHMACOK(t *testing.T) {
	k := []byte("k")
	m := []byte("m")
	mac := RawHMAC(k, m)
	if !VerifyRawHMAC(k, m, mac) {
		t.Fatal("verify should pass")
	}
}

func TestVerifyRawHMACBadKey(t *testing.T) {
	m := []byte("m")
	mac := RawHMAC([]byte("k1"), m)
	if VerifyRawHMAC([]byte("k2"), m, mac) {
		t.Fatal("different keys must not verify")
	}
}

func TestVerifyRawHMACBadMsg(t *testing.T) {
	k := []byte("k")
	mac := RawHMAC(k, []byte("hello"))
	if VerifyRawHMAC(k, []byte("hallo"), mac) {
		t.Fatal("different msgs must not verify")
	}
}

func TestVerifyRawHMACShortMAC(t *testing.T) {
	k := []byte("k")
	m := []byte("m")
	short := RawHMAC(k, m)[:16]
	if VerifyRawHMAC(k, m, short) {
		t.Fatal("short mac must not verify")
	}
}
