package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/printserver/printserver/pkg/config"
	"github.com/printserver/printserver/pkg/printprofile"
)

// cupsSync deixa as filas CUPS iguais às impressoras de printers.json.
//
// Toda a conversão acontece na Pi: o cliente só envia um PDF para a fila IPP.
// O destino da fila é o bridge, que continua sendo o único a escrever na USB.
// Filas que não começam com "ps-" nunca são tocadas, para não atrapalhar
// configuração feita à mão no aparelho.
func cupsSync() error {
	path := os.Getenv("PRINTBOX_PRINTERS")
	if path == "" {
		path = "/etc/printserver/printers.json"
	}
	cfg, err := config.LoadPrinters(path)
	if err != nil {
		return fmt.Errorf("ler impressoras: %w", err)
	}

	existing, err := existingQueues()
	if err != nil {
		return err
	}

	want := map[string]config.Printer{}
	for _, p := range cfg.Printers {
		if p.Enabled {
			want[printprofile.QueueName(p.SerialNumber)] = p
		}
	}

	for q := range existing {
		if strings.HasPrefix(q, "ps-") {
			if _, ok := want[q]; !ok {
				if out, err := exec.Command("/usr/sbin/lpadmin", "-x", q).CombinedOutput(); err != nil {
					return fmt.Errorf("remover fila %s: %w: %s", q, err, strings.TrimSpace(string(out)))
				}
			}
		}
	}

	for q, p := range want {
		uri := deviceURI(p)
		ppd := printprofile.PPDPath(printprofile.Effective(p.Profile, p.Manufacturer, p.Model, p.USBID))
		if err := ensureQueue(q, uri, ppd, existing[q], readDeviceURI(q), readQueuePPD(q)); err != nil {
			return err
		}
	}

	// Permitir que outras máquinas da rede enviem trabalhos para a fila.
	if out, err := exec.Command("/usr/sbin/cupsctl", "--remote-any").CombinedOutput(); err != nil {
		return fmt.Errorf("liberar IPP: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// setPrintProfile grava o perfil escolhido no painel e já aplica a mudança
// nas filas. Roda como root pelo helper, porque printers.json pertence à Pi.
func setPrintProfile(serial, profile string) error {
	if !printprofile.Valid(profile) {
		return fmt.Errorf("perfil inválido")
	}
	if serial == "" || len(serial) > 150 || strings.ContainsAny(serial, "/\\\r\n") {
		return fmt.Errorf("serial inválido")
	}
	path := os.Getenv("PRINTBOX_PRINTERS")
	if path == "" {
		path = "/etc/printserver/printers.json"
	}
	if err := writeProfile(path, serial, profile); err != nil {
		return err
	}
	return cupsSync()
}

func writeProfile(path, serial, profile string) error {
	cfg, err := config.LoadPrinters(path)
	if err != nil {
		return fmt.Errorf("ler impressoras: %w", err)
	}
	found := false
	for i := range cfg.Printers {
		if cfg.Printers[i].SerialNumber == serial && cfg.Printers[i].Enabled {
			cfg.Printers[i].Profile = profile
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("impressora não encontrada")
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func deviceURI(p config.Printer) string {
	return fmt.Sprintf("socket://127.0.0.1:%d", p.TCPPort)
}

func existingQueues() (map[string]bool, error) {
	out, err := exec.Command("/usr/bin/lpstat", "-e").Output()
	if err != nil {
		return nil, fmt.Errorf("listar filas: %w", err)
	}
	queues := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		q := strings.Fields(line)
		if len(q) > 0 {
			queues[q[0]] = true
		}
	}
	return queues, nil
}

func readDeviceURI(queue string) string {
	out, err := exec.Command("/usr/bin/lpstat", "-v", queue).Output()
	if err != nil {
		return ""
	}
	prefix := "device for " + queue + ": "
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); strings.HasPrefix(s, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(s, prefix))
		}
	}
	return ""
}

func readQueuePPD(queue string) []byte {
	b, err := os.ReadFile("/etc/cups/ppd/" + queue + ".ppd")
	if err != nil {
		return nil
	}
	return b
}

func ensureQueue(queue, uri, ppd string, exists bool, currentURI string, currentPPD []byte) error {
	wantPPD, err := os.ReadFile(ppd)
	if err != nil {
		return fmt.Errorf("ler PPD %s: %w", ppd, err)
	}
	if exists && currentURI == uri && bytes.Equal(currentPPD, wantPPD) {
		return nil
	}
	if exists {
		if out, err := exec.Command("/usr/sbin/lpadmin", "-x", queue).CombinedOutput(); err != nil {
			return fmt.Errorf("remover fila %s: %w: %s", queue, err, strings.TrimSpace(string(out)))
		}
	}
	args := []string{"-p", queue, "-E", "-v", uri, "-P", ppd,
		"-o", "printer-is-shared=true", "-o", "media=A4"}
	if out, err := exec.Command("/usr/sbin/lpadmin", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("criar fila %s: %w: %s", queue, err, strings.TrimSpace(string(out)))
	}
	for _, cmd := range [][]string{
		{"/usr/sbin/cupsenable", queue},
		{"/usr/sbin/cupsaccept", queue},
	} {
		if out, err := exec.Command(cmd[0], cmd[1]).CombinedOutput(); err != nil {
			return fmt.Errorf("%s %s: %w: %s", cmd[0], queue, err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
