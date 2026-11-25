package main

import (
	"bufio"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"strings"

	"github.com/printserver/printserver/pkg/crypto"
	"github.com/printserver/printserver/pkg/device"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "init-identity":
		path := "/etc/printserver/device.json"
		if len(os.Args) >= 3 {
			path = os.Args[2]
		}
		if err := initIdentity(path); err != nil {
			die(err)
		}
	case "device-id":
		path := "/etc/printserver/device.json"
		if len(os.Args) >= 3 {
			path = os.Args[2]
		}
		id, err := device.Load(path)
		if err != nil {
			die(err)
		}
		fmt.Println(id.DeviceID)
	case "hash-password":
		if err := hashPassword(); err != nil {
			die(err)
		}
	case "gen-hotspot-password":
		pw, err := crypto.HotspotPassword()
		if err != nil {
			die(err)
		}
		fmt.Println(pw)
	case "fingerprint-sha256":
		if len(os.Args) < 3 {
			die(fmt.Errorf("usage: fingerprint-sha256 FILE"))
		}
		if err := fingerprint(os.Args[2]); err != nil {
			die(err)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `printserver-tool subcommands:
  init-identity [path]        Create device.json (idempotent)
  device-id [path]            Print device_id from device.json
  hash-password               Read password from stdin, print argon2id hash
  gen-hotspot-password        Print an 8-char hotspot password
  fingerprint-sha256 FILE     Print sha256 of a PEM cert
`)
}

func die(err error) {
	fmt.Fprintln(os.Stderr, "printserver-tool:", err)
	os.Exit(1)
}

func initIdentity(path string) error {
	if id, err := device.Load(path); err == nil {
		fmt.Println(id.DeviceID)
		return nil
	}
	model, serial := device.DetectHardware()
	id, err := device.Create(model, serial)
	if err != nil {
		return err
	}
	if err := id.Save(path); err != nil {
		return err
	}
	fmt.Println(id.DeviceID)
	return nil
}

func hashPassword() error {
	sc := bufio.NewReader(os.Stdin)
	line, err := sc.ReadString('\n')
	if err != nil && len(line) == 0 {
		return err
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return fmt.Errorf("empty password")
	}
	phc, err := crypto.HashPassword(pw)
	if err != nil {
		return err
	}
	fmt.Println(phc)
	return nil
}

func fingerprint(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	block, _ := pem.Decode(b)
	if block == nil {
		return fmt.Errorf("not a PEM file")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(cert.Raw)
	fmt.Println(hex.EncodeToString(sum[:]))
	return nil
}
