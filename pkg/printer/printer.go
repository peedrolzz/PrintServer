package printer

import (
	"errors"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type Device struct {
	Path string

	mu    sync.Mutex
	queue chan struct{}
	max   int
}

func New(path string, maxConcurrent int) *Device {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	return &Device{Path: path, queue: make(chan struct{}, maxConcurrent), max: maxConcurrent}
}

var ErrBusy = errors.New("printer: queue full")

var ErrNoDevice = errors.New("printer: device absent")

var ErrTimeout = errors.New("printer: write timed out")

// writerOnly esconde o método ReadFrom de *os.File. Sem isso, o io.Copy usa
// splice(2) quando a origem é um socket TCP; splice não funciona com device
// de caractere (usblp) e falha com "splice: invalid argument".
type writerOnly struct{ io.Writer }

func copyJob(dst io.Writer, src io.Reader, max int64) (int64, error) {
	return io.Copy(dst, io.LimitReader(src, max))
}

func (d *Device) WriteJob(src io.Reader, max int64, deadline time.Duration) (int64, error) {
	select {
	case d.queue <- struct{}{}:
		defer func() { <-d.queue }()
	default:
		return 0, ErrBusy
	}
	d.mu.Lock()
	defer d.mu.Unlock()

	if _, err := os.Stat(d.Path); err != nil {
		return 0, ErrNoDevice
	}
	fd, err := os.OpenFile(d.Path, os.O_WRONLY|syscall.O_NOCTTY, 0)
	if err != nil {
		return 0, err
	}
	defer fd.Close()

	// Fechar o descritor a partir de outro goroutine desbloqueia um write(2)
	// preso na USB e impede que a fila fique travada para sempre.
	var timedOut atomic.Bool
	timer := time.AfterFunc(deadline, func() {
		timedOut.Store(true)
		_ = fd.Close()
	})
	defer timer.Stop()

	n, err := copyJob(writerOnly{fd}, src, max)
	if timedOut.Load() {
		return n, ErrTimeout
	}
	return n, err
}
