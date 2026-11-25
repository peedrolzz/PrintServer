package crypto

import "testing"

func TestHashVerifyRoundtrip(t *testing.T) {
	phc, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := VerifyPassword("correct horse battery staple", phc)
	if err != nil {
		t.Fatalf("VerifyPassword: %v", err)
	}
	if !ok {
		t.Fatalf("expected match")
	}
}

func TestVerifyRejectsWrongPassword(t *testing.T) {
	phc, _ := HashPassword("abcdef")
	ok, _ := VerifyPassword("abcdeg", phc)
	if ok {
		t.Fatalf("expected mismatch")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	for _, bad := range []string{"", "$argon2i$", "notphc", "$argon2id$v=19$m=10,t=1,p=1$short$short"} {
		if _, err := VerifyPassword("x", bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestHotspotPasswordFormat(t *testing.T) {
	for i := 0; i < 50; i++ {
		p, err := HotspotPassword()
		if err != nil {
			t.Fatalf("gen: %v", err)
		}
		if len(p) != 8 {
			t.Fatalf("len=%d want 8: %q", len(p), p)
		}
		for _, c := range p {
			if c == '0' || c == '1' || c == 'I' || c == 'O' || c == '-' {
				t.Fatalf("ambiguous char %q in %q", c, p)
			}
			if !((c >= 'A' && c <= 'Z') || (c >= '2' && c <= '9')) {
				t.Fatalf("unexpected char %q in %q", c, p)
			}
		}
	}
}

func TestBytesLength(t *testing.T) {
	b, err := Bytes(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != 32 {
		t.Fatalf("len=%d", len(b))
	}
}
