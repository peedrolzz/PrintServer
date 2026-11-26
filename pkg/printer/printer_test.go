package printer

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestCopyJobNaoExpoeReadFrom(t *testing.T) {
	if _, ok := any(writerOnly{os.Stdout}).(io.ReaderFrom); ok {
		t.Fatal("writerOnly não deve expor ReadFrom, senão o io.Copy tenta splice")
	}
}

func TestCopyJobRespeitaLimite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "job.bin")
	fd, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fd.Close()

	entrada := []byte("0123456789")
	n, err := copyJob(writerOnly{fd}, bytes.NewReader(entrada), 5)
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatalf("copiou %d bytes, esperado 5", n)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "01234" {
		t.Fatalf("conteúdo = %q, esperado %q", got, "01234")
	}
}
