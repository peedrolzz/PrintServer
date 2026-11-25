package printer

import (
	"errors"
	"io"
	"os"
	"sync"
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

	limited := io.LimitReader(src, max)
	_ = deadline
	return io.Copy(fd, limited)
}
