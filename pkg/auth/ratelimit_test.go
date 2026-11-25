package auth

import (
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
