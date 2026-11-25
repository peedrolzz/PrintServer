package device

import (
	"path/filepath"
	"testing"
)

func TestCreateHasPrefixAndUUID(t *testing.T) {
	id, err := Create("Raspberry Pi 3B+", "0000000000000001")
	if err != nil {
		t.Fatal(err)
	}
	if len(id.DeviceID) < 6 || id.DeviceID[:3] != "PS-" {
		t.Fatalf("device_id missing PS- prefix: %q", id.DeviceID)
	}
	if len(id.DeviceUUID) != 36 {
		t.Fatalf("uuid wrong len: %q", id.DeviceUUID)
	}
}

func TestLoadRoundtrip(t *testing.T) {
	id, err := Create("Pi", "SN")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "device.json")
	if err := id.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != id.DeviceID || got.DeviceUUID != id.DeviceUUID {
		t.Fatalf("mismatch: %+v vs %+v", got, id)
	}
}

func TestShortSuffixLen(t *testing.T) {
	id := &Identity{DeviceID: "PS-ABCDEF123456"}
	if s := id.ShortSuffix(); s != "3456" {
		t.Fatalf("suffix=%q", s)
	}
	short := &Identity{DeviceID: "PS-AB"}
	if s := short.ShortSuffix(); s != "SETUP" {
		t.Fatalf("suffix=%q", s)
	}
}
