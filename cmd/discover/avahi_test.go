package main

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/printserver/printserver/pkg/config"
)

func TestPublishAvahi(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Printers{Printers: []config.Printer{
		{Name: "RICOH SP", Manufacturer: "RICOH", Model: "SP", TCPPort: 9100, Enabled: true},
		{Name: "Desligada", TCPPort: 9101, Enabled: false},
	}}

	if err := publishAvahi(cfg, dir, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, avahiFile))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "_pdl-datastream._tcp") {
		t.Fatalf("serviço JetDirect ausente: %s", got)
	}
	if !strings.Contains(got, "<port>9100</port>") {
		t.Fatalf("porta 9100 ausente: %s", got)
	}
	if strings.Contains(got, "<port>9101</port>") {
		t.Fatalf("impressora desativada foi anunciada: %s", got)
	}
	if !strings.Contains(got, "_ipp._tcp") {
		t.Fatalf("serviço IPP ausente: %s", got)
	}
	if !strings.Contains(got, "<port>631</port>") {
		t.Fatalf("porta IPP ausente: %s", got)
	}
	if !strings.Contains(got, "rp=printers/printserver") {
		t.Fatalf("fila IPP ausente: %s", got)
	}
	var doc struct {
		XMLName  xml.Name `xml:"service-group"`
		Services []struct {
			Name string `xml:"name"`
			Type string `xml:"type"`
		} `xml:"service"`
	}
	if err := xml.Unmarshal([]byte(got), &doc); err != nil {
		t.Fatalf("XML do avahi inválido: %v", err)
	}
	if len(doc.Services) != 2 {
		t.Fatalf("esperados 2 serviços, vieram %d", len(doc.Services))
	}
	for _, svc := range doc.Services {
		if svc.Name != "PrintServer" {
			t.Fatalf("nome anunciado deve ser PrintServer, veio %q", svc.Name)
		}
	}
	if strings.Contains(got, "<name>ps-") {
		t.Fatalf("nome da fila vazou no anúncio: %s", got)
	}
}

func TestPublishAvahiNomesUnicos(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Printers{Printers: []config.Printer{
		{Name: "Primeira", SerialNumber: "AAA", TCPPort: 9100, Enabled: true},
		{Name: "Segunda", SerialNumber: "BBB", TCPPort: 9101, Enabled: true},
	}}
	if err := publishAvahi(cfg, dir, true); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, avahiFile))
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.Contains(got, "<name>PrintServer</name>") {
		t.Fatalf("primeiro nome ausente: %s", got)
	}
	if !strings.Contains(got, "<name>PrintServer 2</name>") {
		t.Fatalf("segundo nome ausente: %s", got)
	}
}

func TestPublishAvahiDesligado(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Printers{Printers: []config.Printer{
		{Name: "RICOH", TCPPort: 9100, Enabled: true},
	}}
	if err := publishAvahi(cfg, dir, true); err != nil {
		t.Fatal(err)
	}
	if err := publishAvahi(cfg, dir, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, avahiFile)); !os.IsNotExist(err) {
		t.Fatalf("anúncio deveria ter sido removido: %v", err)
	}
}

func TestDiscoveryEnabled(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "discovery-off")
	if !discoveryEnabled(marker) {
		t.Fatal("sem marcador a descoberta deve ficar ligada")
	}
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if discoveryEnabled(marker) {
		t.Fatal("com marcador a descoberta deve ficar desligada")
	}
}
