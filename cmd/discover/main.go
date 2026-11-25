package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/printserver/printserver/internal/version"
	"github.com/printserver/printserver/pkg/config"
	"github.com/printserver/printserver/pkg/helperclient"
	"github.com/printserver/printserver/pkg/logger"
)

const (
	defaultConfig = "/etc/printserver/printers.json"
	portStart     = 9100
	portEnd       = 9199
)

var serialRE = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,150}$`)

func main() {
	cfgPath := flag.String("config", envOr("PRINTBOX_PRINTERS", defaultConfig), "printers.json path")
	noRestart := flag.Bool("no-restart", false, "do not signal bridge to reload")
	showVer := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVer {
		fmt.Println(version.String())
		return
	}

	log := logger.Default()
	log.Info("discover starting", slog.String("version", version.Version))

	devices := listUSBPrinters()
	log.Info("discover: devices seen", slog.Int("count", len(devices)))

	cfg, err := config.LoadPrinters(*cfgPath)
	if err != nil {
		log.Warn("discover: existing config invalid, starting empty", "err", err)
		cfg = &config.Printers{}
	}
	changed := mergeConfig(cfg, devices, log)
	if !changed {
		log.Info("discover: no changes")
		return
	}

	if err := atomicSave(*cfgPath, cfg); err != nil {
		log.Error("discover: save failed", "err", err)
		os.Exit(1)
	}
	log.Info("discover: config updated",
		slog.Int("printers", len(cfg.Printers)),
	)

	if !*noRestart {
		if err := signalReload(); err != nil {
			log.Warn("discover: signal bridge failed", "err", err)
		}
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

type usbDevice struct {
	Name         string
	Manufacturer string
	Model        string
	USBID        string
	Serial       string
	DevicePath   string
}

func listUSBPrinters() []usbDevice {
	matches, _ := filepath.Glob("/dev/usb/lp*")
	var out []usbDevice
	for _, m := range matches {
		d, ok := readUdevProps(m)
		if !ok {
			continue
		}
		out = append(out, d)
	}
	return out
}

func readUdevProps(path string) (usbDevice, bool) {
	cmd := exec.Command("/usr/bin/udevadm", "info", "--query=property", "--name", path)
	out, err := cmd.Output()
	if err != nil {
		return usbDevice{}, false
	}
	var d usbDevice
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(k) {
		case "ID_SERIAL_SHORT":
			d.Serial = sanitize(v, 150)
		case "ID_VENDOR_ID":
			if d.USBID == "" {
				d.USBID = v
			} else {
				d.USBID = v + ":" + d.USBID
			}
		case "ID_MODEL_ID":
			if d.USBID == "" {
				d.USBID = v
			} else {
				d.USBID = d.USBID + ":" + v
			}
		case "ID_VENDOR":
			d.Manufacturer = sanitize(v, 80)
		case "ID_MODEL":
			d.Model = sanitize(v, 80)
		}
	}
	if d.Serial == "" || !serialRE.MatchString(d.Serial) {
		return usbDevice{}, false
	}
	if d.Model != "" && d.Manufacturer != "" {
		d.Name = d.Manufacturer + " " + d.Model
	} else if d.Model != "" {
		d.Name = d.Model
	} else {
		d.Name = "Impressora USB " + d.Serial
	}
	d.DevicePath = "/dev/printserver/by-serial/" + d.Serial
	return d, true
}

func sanitize(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\x00", "")
	if len(s) > max {
		s = s[:max]
	}
	return s
}

func mergeConfig(cfg *config.Printers, devs []usbDevice, log *slog.Logger) bool {
	changed := false
	bySerial := map[string]int{}
	usedPorts := map[int]bool{}
	for i, p := range cfg.Printers {
		bySerial[p.SerialNumber] = i
		usedPorts[p.TCPPort] = true
	}
	for _, d := range devs {
		idx, exists := bySerial[d.Serial]
		if exists {
			old := cfg.Printers[idx]
			old.Name = d.Name
			old.Manufacturer = d.Manufacturer
			old.Model = d.Model
			old.USBID = d.USBID
			old.DevicePath = d.DevicePath
			if !equal(cfg.Printers[idx], old) {
				cfg.Printers[idx] = old
				changed = true
			}
			continue
		}
		port := nextFreePort(usedPorts)
		if port == 0 {
			log.Warn("discover: no free port", slog.String("serial_sh", d.Serial[:min(6, len(d.Serial))]))
			continue
		}
		usedPorts[port] = true
		cfg.Printers = append(cfg.Printers, config.Printer{
			Name:         d.Name,
			Manufacturer: d.Manufacturer,
			Model:        d.Model,
			USBID:        d.USBID,
			SerialNumber: d.Serial,
			DevicePath:   d.DevicePath,
			TCPPort:      port,
			Enabled:      true,
		})
		changed = true
	}
	if changed {
		cfg.ConfigRevision++
	}
	return changed
}

func equal(a, b config.Printer) bool {
	return a == b
}

func nextFreePort(used map[int]bool) int {
	for p := portStart; p <= portEnd; p++ {
		if !used[p] {
			return p
		}
	}
	return 0
}

func atomicSave(path string, cfg *config.Printers) error {
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

func signalReload() error {
	_, err := helperclient.Send("", "reload-bridge-config")
	return err
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
