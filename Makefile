# PrintServer build pipeline.
#
# Default target: cross-compiled arm64 binaries ready for Raspberry Pi 3B+.
# `make deb` packages them into a .deb that drops cleanly into the imager.

VERSION ?= 0.3.0
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo manual)
DATE    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X github.com/printserver/printserver/internal/version.Version=$(VERSION) \
  -X github.com/printserver/printserver/internal/version.Commit=$(COMMIT) \
  -X github.com/printserver/printserver/internal/version.BuildDate=$(DATE)

GOFLAGS := -trimpath -mod=readonly
TARGETS := bridge portal discover helper tool
BIN_DIR := dist/bin/arm64
PKG_DIR := dist/pkg

export CGO_ENABLED=0
export GOOS=linux
export GOARCH=arm64

.PHONY: all
all: $(addprefix $(BIN_DIR)/, $(addprefix printserver-, $(TARGETS)))

$(BIN_DIR)/printserver-%:
	@mkdir -p $(BIN_DIR)
	go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o $@ ./cmd/$*

.PHONY: host
host:
	@mkdir -p dist/bin/host
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o dist/bin/host/printserver-bridge   ./cmd/bridge
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o dist/bin/host/printserver-portal   ./cmd/portal
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o dist/bin/host/printserver-discover ./cmd/discover
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o dist/bin/host/printserver-helper   ./cmd/helper
	GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -ldflags='$(LDFLAGS)' -o dist/bin/host/printserver-tool     ./cmd/tool

.PHONY: deb
deb: all
	@rm -rf $(PKG_DIR)/printserver_$(VERSION)_arm64
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/bin
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/udev/rules.d
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/printserver
	@mkdir -p $(PKG_DIR)/printserver_$(VERSION)_arm64/var/lib/printserver
	install -m 0755 $(BIN_DIR)/printserver-bridge   $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/bin/
	install -m 0755 $(BIN_DIR)/printserver-portal   $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/bin/
	install -m 0755 $(BIN_DIR)/printserver-discover $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/bin/
	install -m 0755 $(BIN_DIR)/printserver-helper   $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/bin/
	install -m 0755 $(BIN_DIR)/printserver-tool     $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/bin/
	install -m 0755 scripts/printserver-firstboot              $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	# seal-image é ferramenta da estação de imagem: nunca vai para a Pi de campo.
	install -m 0755 scripts/printserver-apply-network-config   $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-update-issue           $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-set-mode               $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-set-bridge-mode        $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-watchdog               $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-recovery               $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-wifi-scan              $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-hostname               $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0755 scripts/printserver-wifi-profile           $(PKG_DIR)/printserver_$(VERSION)_arm64/usr/local/sbin/
	install -m 0644 systemd/printserver-bridge.service    $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-portal.service    $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-discover.service  $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-firstboot.service $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-helper.service         $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-network.service        $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-hotspot.service        $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-hotspot-dns.service    $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-issue.service          $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-watchdog.service       $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-watchdog.timer         $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-recovery.service       $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-hostname.service       $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 systemd/printserver-wifi-scan.service      $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/systemd/system/
	install -m 0644 udev/90-printserver.rules                  $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/udev/rules.d/
	install -m 0755 scripts/deb-postinst $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/postinst
	install -m 0755 scripts/deb-postrm   $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/postrm
	printf '{\n  "printers": [],\n  "config_revision": 0\n}\n' > $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/printserver/printers.json
	chmod 0644 $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/printserver/printers.json
	install -m 0644 config/nftables.conf $(PKG_DIR)/printserver_$(VERSION)_arm64/etc/printserver/nftables.conf
	# Estes dois arquivos são configurados na Pi. Sem conffiles o dpkg
	# sobrescreve a lista de impressoras e as regras locais em cada upgrade.
	printf '/etc/printserver/printers.json\n/etc/printserver/nftables.conf\n' > $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/conffiles
	# NOTE: firstboot.flag is created by the postinst only on FRESH installs,
	# not on upgrades, so cloned SDs still boot with per-device credentials
	# but upgrading an already provisioned Pi does NOT re-run firstboot.
	echo "Package: printserver"             >  $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Version: $(VERSION)"              >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Section: net"                     >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Priority: optional"               >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Architecture: arm64"              >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Maintainer: PrintServer <printserver@example.org>" >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Depends: systemd, network-manager, hostapd, dnsmasq, avahi-daemon, openssl, rfkill, iproute2, iw" >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	echo "Description: PrintServer, provisional USB print service for Raspberry Pi 3" >> $(PKG_DIR)/printserver_$(VERSION)_arm64/DEBIAN/control
	dpkg-deb --build --root-owner-group $(PKG_DIR)/printserver_$(VERSION)_arm64

.PHONY: test
test:
	GOOS=linux GOARCH=amd64 go test $(GOFLAGS) ./...

.PHONY: lint
lint:
	go vet ./...
	gofmt -l . | tee /dev/stderr | awk 'NF{exit 1}'

.PHONY: ci-scan
ci-scan:
	@echo "== CI image security scan =="
	@! grep -rE 'NOPASSWD:\s*ALL' systemd udev scripts | grep -vE '^[^:]+:\s*#|audit\.sh:|"[^"]*NOPASSWD' || (echo 'sudo NOPASSWD ALL found'; exit 1)
	@! find . -name '.codex' -o -name '.cursor' -o -name '.claude' -o -name '.bash_history' -o -name '*.backup*' -o -name '*.bak' | grep -q . || (echo 'developer artefact found'; exit 1)
	@! grep -rE 'AGENT_TOKEN\s*=\s*"[^"]+"' . 2>/dev/null | grep -v _test.go | grep -q . || (echo 'hardcoded token'; exit 1)
	@echo "ci-scan ok"

.PHONY: clean
clean:
	rm -rf dist
