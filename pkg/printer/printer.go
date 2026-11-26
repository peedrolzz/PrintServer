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

	limited := io.LimitReader(src, max)
	n, err := io.Copy(fd, limited)
	if timedOut.Load() {
		return n, ErrTimeout
	}
	return n, err
}
