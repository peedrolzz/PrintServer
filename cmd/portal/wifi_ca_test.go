package main

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func caRequest(t *testing.T, name, content string) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	f, err := w.CreateFormFile("ca_file", name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/wifi/connect", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	return r
}

func TestSaveUploadedCA(t *testing.T) {
	dir := t.TempDir()
	a := &app{caStore: dir}

	plain := httptest.NewRequest("POST", "/wifi/connect", strings.NewReader("ssid=WiFi-CISS"))
	plain.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := a.saveUploadedCA(plain, "WiFi-CISS"); err != nil {
		t.Fatalf("missing file should be ignored: %v", err)
	}

	bad := caRequest(t, "ca.pem", "não é um certificado")
	if err := a.saveUploadedCA(bad, "WiFi-CISS"); err == nil {
		t.Fatal("non-PEM file must be rejected")
	}

	pem := "-----BEGIN CERTIFICATE-----\nQUJD\n-----END CERTIFICATE-----\n"
	if err := a.saveUploadedCA(caRequest(t, "ca.pem", pem), "WiFi-CISS"); err != nil {
		t.Fatalf("valid PEM rejected: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "WiFi-CISS.pem"))
	if err != nil {
		t.Fatalf("CA not saved under safe filename: %v", err)
	}
	if string(got) != pem {
		t.Fatalf("saved CA changed: %q", got)
	}
}
