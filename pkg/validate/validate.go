package validate

import (
	"net"
	"regexp"
	"strings"
	"unicode/utf8"
)

func SSID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 32 || !utf8.ValidString(s) {
		return ""
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return ""
		}
	}
	return s
}

func Password(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 64 {
		return ""
	}
	return s
}

func ADUser(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 128 {
		return ""
	}
	if !adUserRE.MatchString(s) {
		return ""
	}
	return s
}

var adUserRE = regexp.MustCompile(`^[A-Za-z0-9._\-\\@]+$`)

func IPNet(s string) (*net.IPNet, error) {
	_, n, err := net.ParseCIDR(s)
	return n, err
}
