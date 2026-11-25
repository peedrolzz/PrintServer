package device

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

type Identity struct {
	SchemaVersion  int       `json:"schema_version"`
	DeviceID       string    `json:"device_id"`
	DeviceUUID     string    `json:"device_uuid"`
	HardwareModel  string    `json:"hardware_model,omitempty"`
	HardwareSerial string    `json:"hardware_serial,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

const schemaVersion = 1

func Load(path string) (*Identity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var id Identity
	if err := json.Unmarshal(b, &id); err != nil {
		return nil, fmt.Errorf("parse identity: %w", err)
	}
	if id.DeviceID == "" || id.DeviceUUID == "" {
		return nil, errors.New("identity missing device_id/device_uuid")
	}
	return &id, nil
}

func Create(hwModel, hwSerial string) (*Identity, error) {
	uuidB := make([]byte, 16)
	if _, err := rand.Read(uuidB); err != nil {
		return nil, err
	}
	uuidB[6] = (uuidB[6] & 0x0f) | 0x40
	uuidB[8] = (uuidB[8] & 0x3f) | 0x80
	deviceUUID := fmt.Sprintf("%x-%x-%x-%x-%x",
		uuidB[0:4], uuidB[4:6], uuidB[6:8], uuidB[8:10], uuidB[10:16])
	deviceID := "PS-" + strings.ToUpper(hex.EncodeToString(uuidB[:6]))
	return &Identity{
		SchemaVersion:  schemaVersion,
		DeviceID:       deviceID,
		DeviceUUID:     deviceUUID,
		HardwareModel:  hwModel,
		HardwareSerial: hwSerial,
		CreatedAt:      time.Now().UTC(),
	}, nil
}

func (id *Identity) Save(path string) error {
	b, err := json.MarshalIndent(id, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (id *Identity) ShortSuffix() string {
	core := strings.TrimPrefix(id.DeviceID, "PS-")
	if len(core) >= 4 {
		return core[len(core)-4:]
	}
	return "SETUP"
}

func DetectHardware() (model, serial string) {
	if b, err := os.ReadFile("/proc/device-tree/model"); err == nil {
		model = strings.TrimRight(strings.ReplaceAll(string(b), "\x00", ""), " \n")
	}
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(strings.ToLower(line), "serial") {
				if idx := strings.Index(line, ":"); idx > 0 {
					serial = strings.TrimSpace(line[idx+1:])
				}
			}
		}
	}
	return
}
