package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

type Manager struct {
	SchemaVersion int    `json:"schema_version"`
	BaseURL       string `json:"manager_base_url"`
	InstanceCode  string `json:"instance_code,omitempty"`
	CAFingerprint string `json:"ca_fingerprint_sha256"`
	ConfiguredAt  string `json:"configured_at"`
}

func LoadManager(path string) (*Manager, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m Manager
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manager: %w", err)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manager) Validate() error {
	u, err := url.Parse(m.BaseURL)
	if err != nil {
		return fmt.Errorf("manager_base_url: %w", err)
	}
	if u.Scheme != "https" {
		return errors.New("manager_base_url must use https (V14 mitigation)")
	}
	if u.Host == "" {
		return errors.New("manager_base_url missing host")
	}
	m.BaseURL = strings.TrimRight(m.BaseURL, "/")
	if !fingerprintRE.MatchString(m.CAFingerprint) {
		return errors.New("ca_fingerprint_sha256 must be 64 hex chars (SHA-256)")
	}
	return nil
}

var fingerprintRE = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)

type Printer struct {
	Name         string `json:"name"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Model        string `json:"model,omitempty"`
	USBID        string `json:"usb_id,omitempty"`
	SerialNumber string `json:"serial_number"`
	DevicePath   string `json:"device_path"`
	TCPPort      int    `json:"tcp_port"`
	Enabled      bool   `json:"enabled"`
}

type Printers struct {
	Printers       []Printer `json:"printers"`
	ConfigRevision int64     `json:"config_revision"`
}

func LoadPrinters(path string) (*Printers, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Printers
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("parse printers: %w", err)
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	return &p, nil
}

func (p *Printers) Validate() error {
	if len(p.Printers) > 100 {
		return errors.New("printers: too many entries (max 100)")
	}
	seenSerial := map[string]bool{}
	seenPort := map[int]bool{}
	for i, pr := range p.Printers {
		if !serialRE.MatchString(pr.SerialNumber) {
			return fmt.Errorf("printers[%d]: invalid serial", i)
		}
		if seenSerial[pr.SerialNumber] {
			return fmt.Errorf("printers[%d]: duplicate serial", i)
		}
		seenSerial[pr.SerialNumber] = true
		expected := "/dev/printserver/by-serial/" + pr.SerialNumber
		if pr.DevicePath != "" && pr.DevicePath != expected {
			return fmt.Errorf("printers[%d]: device_path must be %q", i, expected)
		}
		if pr.DevicePath == "" {
			p.Printers[i].DevicePath = expected
		}
		if pr.TCPPort < 9100 || pr.TCPPort > 9199 {
			return fmt.Errorf("printers[%d]: tcp_port must be in [9100,9199]", i)
		}
		if seenPort[pr.TCPPort] {
			return fmt.Errorf("printers[%d]: duplicate tcp_port", i)
		}
		seenPort[pr.TCPPort] = true
		if strings.TrimSpace(pr.Name) == "" {
			return fmt.Errorf("printers[%d]: name required", i)
		}
		for _, s := range []string{pr.Name, pr.Manufacturer, pr.Model, pr.USBID} {
			if strings.ContainsRune(s, 0) {
				return fmt.Errorf("printers[%d]: null byte in string", i)
			}
		}
	}
	return nil
}

var serialRE = regexp.MustCompile(`^[A-Za-z0-9_.\-]{1,150}$`)

func (p *Printers) Save(path string) error {
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
