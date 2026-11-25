package main

import "testing"

func TestStatusSummary(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		ip, printer, internet bool
		label, detail, level  string
	}{
		{"pronto", true, true, true, "Pronto", "", "ok"},
		{"sem internet", true, true, false, "Pronto", "Sem internet. A impressão local continua funcionando.", "warn"},
		{"sem impressora", true, false, true, "Sem impressora", "Ligue uma impressora USB e use Reconhecer USB em Manutenção.", "warn"},
		{"sem rede", false, true, true, "Sem rede", "Ligue o cabo ou configure o WiFi.", "err"},
		{"sem nada", false, false, false, "Sem rede nem impressora", "Ligue o cabo ou configure o WiFi e conecte uma impressora USB.", "err"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			label, detail, level := statusSummary(tc.ip, tc.printer, tc.internet)
			if label != tc.label || detail != tc.detail || level != tc.level {
				t.Fatalf("got (%q, %q, %q), want (%q, %q, %q)",
					label, detail, level, tc.label, tc.detail, tc.level)
			}
		})
	}
}
