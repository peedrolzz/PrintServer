package validate

import "testing"

func TestSSID(t *testing.T) {
	if SSID("ok-ssid") == "" {
		t.Fatal("expected accepted")
	}
	if SSID("") != "" {
		t.Fatal("empty rejected")
	}
	if SSID("with\nnewline") != "" {
		t.Fatal("V22: newline must be rejected")
	}
	if SSID("with\x00null") != "" {
		t.Fatal("null byte must be rejected")
	}
	if SSID("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") != "" {
		t.Fatal("too long rejected")
	}
}

func TestADUser(t *testing.T) {
	for _, good := range []string{"joao", "DOMAIN\\joao", "joao@example.com", "user.name_1"} {
		if ADUser(good) == "" {
			t.Fatalf("expected ok: %q", good)
		}
	}
	for _, bad := range []string{"", "has space", "j;rm -rf", "a\nb"} {
		if ADUser(bad) != "" {
			t.Fatalf("expected bad: %q", bad)
		}
	}
}

func TestPassword(t *testing.T) {
	if Password("") != "" {
		t.Fatal("empty rejected")
	}
	if Password("ok") == "" {
		t.Fatal("short is ok")
	}
	long := make([]byte, 65)
	for i := range long {
		long[i] = 'x'
	}
	if Password(string(long)) != "" {
		t.Fatal("over-long rejected")
	}
}
