package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"html"
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
	defaultConfig    = "/etc/printserver/printers.json"
	defaultAvahiDir  = "/etc/avahi/services"
	avahiFile        = "printserver.service"
	defaultOffMarker = "/etc/printserver/discovery-off"
	portStart        = 9100
	portEnd          = 9199
)

var serialRE = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,150}$`)

func main() {
	cfgPath := flag.String("config", envOr("PRINTBOX_PRINTERS", defaultConfig), "printers.json path")
	avahiDir := flag.String("avahi-dir", envOr("PRINTBOX_AVAHI_DIR", defaultAvahiDir), "avahi services directory")
	offMarker := flag.String("discovery-off", envOr("PRINTBOX_DISCOVERY_OFF", defaultOffMarker), "marker that disables automatic discovery")
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
		// Nunca substituir uma config corrompida por uma vazia: isso apagaria
		// serial e porta das impressoras já pareadas após uma queda de energia.
		log.Error("discover: existing config invalid, refusing to overwrite", "err", err)
		os.Exit(1)
	}
	changed := mergeConfig(cfg, devices, log)
	if changed {
		if err := atomicSave(*cfgPath, cfg); err != nil {
			log.Error("discover: save failed", "err", err)
			os.Exit(1)
		}
		log.Info("discover: config updated",
			slog.Int("printers", len(cfg.Printers)),
		)
	} else {
		log.Info("discover: no changes")
	}

	// Publicar mesmo sem mudança: o arquivo pode ter sido apagado ou a
	// descoberta automática pode ter sido reativada desde o boot anterior.
	if err := publishAvahi(cfg, *avahiDir, discoveryEnabled(*offMarker)); err != nil {
		log.Warn("discover: publish avahi failed", "err", err)
	}

	if changed && !*noRestart {
		if err := signalReload(); err != nil {
			log.Warn("discover: signal bridge failed", "err", err)
		}
	}
}

// discoveryEnabled devolve false somente quando o operador desligou a
// descoberta automática no painel. Sem marcador, a impressora aparece na rede.
func discoveryEnabled(marker string) bool {
	_, err := os.Stat(marker)
	return err != nil
}

// publishAvahi anuncia cada impressora ativa como uma impressora JetDirect na
// porta TCP correspondente. Quando desligado, remove o anúncio do avahi para
// que só a configuração manual continue funcionando.
func publishAvahi(cfg *config.Printers, dir string, enabled bool) error {
	path := filepath.Join(dir, avahiFile)
	if !enabled {
		err := os.Remove(path)
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" standalone='no'?>\n")
	b.WriteString("<!DOCTYPE service-group SYSTEM \"avahi-service.dtd\">\n")
	b.WriteString("<service-group>\n")
	b.WriteString("  <name>PrintServer</name>\n")
	for _, p := range cfg.Printers {
		if !p.Enabled {
			continue
		}
		fmt.Fprintf(&b, "  <service>\n")
		b.WriteString("    <type>_pdl-datastream._tcp</type>\n")
		fmt.Fprintf(&b, "    <port>%d</port>\n", p.TCPPort)
		fmt.Fprintf(&b, "    <txt-record>ty=%s</txt-record>\n", "Impressora PrintServer")
		fmt.Fprintf(&b, "    <txt-record>usb=MFG:%s;MDL:%s;DES:%s;CMD:PCL</txt-record>\n",
			avahiText(p.Manufacturer), avahiText(p.Model), avahiText(p.Name))
		b.WriteString("  </service>\n")
	}
	b.WriteString("</service-group>\n")

	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// avahiText mantém os campos TXT em ASCII seguro, sem ';' que quebraria o
// formato usb= e sem caracteres de controle.
func avahiText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0x20 && r <= 0x7e && r != ';' {
			b.WriteRune(r)
		} else {
			b.WriteByte(' ')
		}
	}
	v := b.String()
	if len(v) > 120 {
		v = v[:120]
	}
	return html.EscapeString(v)
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
	tmp := fmt.Sprintf("%s.tmp.%d", path, os.Getpid())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	// Sem o sync do diretório o rename pode não sobreviver a uma queda.
	dir, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
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
