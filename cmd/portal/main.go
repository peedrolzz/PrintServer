package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/printserver/printserver/internal/version"
	"github.com/printserver/printserver/pkg/auth"
	"github.com/printserver/printserver/pkg/bridgeack"
	"github.com/printserver/printserver/pkg/config"
	"github.com/printserver/printserver/pkg/crypto"
	"github.com/printserver/printserver/pkg/device"
	"github.com/printserver/printserver/pkg/helperclient"
	"github.com/printserver/printserver/pkg/logger"
	"github.com/printserver/printserver/pkg/validate"
)

//go:embed templates/*.html
var tplFS embed.FS

const (
	defaultConfigDir     = "/etc/printserver"
	defaultStateDir      = "/var/lib/printserver"
	defaultTLSCert       = "/var/lib/printserver/credentials/portal-cert.pem"
	defaultTLSKey        = "/var/lib/printserver/credentials/portal-key.pem"
	initialAdminPassword = "printserver"
	defaultAdminHash     = "/var/lib/printserver/credentials/portal-admin"
	defaultHotspotPW     = "/var/lib/printserver/credentials/hotspot-password"
	defaultCAStore       = "/var/lib/printserver/credentials/enterprise-ca"
	defaultBridgeTok     = "/var/lib/printserver/credentials/bridge-token"
	bridgeTokenModePath  = "/etc/printserver/bridge-token-mode"
	discoveryOffPath     = "/etc/printserver/discovery-off"
	defaultFirstRun      = "/var/lib/printserver/credentials/first-run.flag"
	defaultListen        = "0.0.0.0:443"
	maxRequestBody       = 16 * 1024
	maxCAUpload          = 32 * 1024
	readHeaderTimeout    = 5 * time.Second
	writeTimeout         = 15 * time.Second
	idleTimeout          = 30 * time.Second
	sessionTTL           = 2 * time.Hour
	maxLoginPerMinute    = 10
	maxConnsConcurr      = 25
	// Depois de trocar de rede o hotspot cai. Oito segundos dão tempo para
	// ler e tocar no endereço antes que o painel fique fora do ar.
	wifiSwitchDelay = 8 * time.Second
)

type app struct {
	log                 *slog.Logger
	sess                *auth.Manager
	loginLimit          *auth.Limiter
	actionsLimit        *auth.Limiter
	adminHashPath       string
	hotspotPasswordPath string
	caStore             string
	configDir           string
	bridgeTokPath       string
	firstRunPath        string
	deviceID            string
	dev                 bool
	netMu               sync.Mutex
}

func main() {
	configDir := flag.String("config-dir", envOr("PRINTBOX_CONFIG_DIR", defaultConfigDir), "config dir")
	stateDir := flag.String("state-dir", envOr("PRINTBOX_STATE_DIR", defaultStateDir), "state dir")
	certFile := flag.String("tls-cert", envOr("PRINTBOX_TLS_CERT", defaultTLSCert), "TLS cert")
	keyFile := flag.String("tls-key", envOr("PRINTBOX_TLS_KEY", defaultTLSKey), "TLS key")
	adminHash := flag.String("admin-hash", envOr("PRINTBOX_ADMIN_HASH", defaultAdminHash), "Argon2id hash of admin password")
	hotspotPW := flag.String("hotspot-password", envOr("PRINTBOX_HOTSPOT_PASSWORD", defaultHotspotPW), "file with the hotspot WPA password")
	listen := flag.String("listen", envOr("PRINTBOX_LISTEN", defaultListen), "listen address")
	caStore := flag.String("ca-store", envOr("PRINTBOX_CA_STORE", defaultCAStore), "dir for uploaded enterprise CAs")
	bridgeTok := flag.String("bridge-token", envOr("PRINTBOX_BRIDGE_TOKEN", defaultBridgeTok), "bridge HMAC token (for test prints)")
	devMode := flag.Bool("dev", false, "development mode: HTTP :8080, password 'devpass'")
	showVer := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVer {
		fmt.Println(version.String())
		return
	}

	log := logger.Default()
	log.Info("portal starting",
		slog.String("version", version.Version),
		slog.String("listen", *listen),
	)

	id, err := device.Load(filepath.Join(*configDir, "device.json"))
	if err != nil {
		log.Warn("portal: device.json missing, using placeholder", "err", err)
		id = &device.Identity{DeviceID: "unknown"}
	}

	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		log.Error("portal: rand failed", "err", err)
		os.Exit(1)
	}

	a := &app{
		log:                 log,
		sess:                auth.NewManager(key, sessionTTL),
		loginLimit:          auth.NewLimiter(time.Minute, maxLoginPerMinute),
		actionsLimit:        auth.NewLimiter(time.Minute, 20),
		adminHashPath:       *adminHash,
		hotspotPasswordPath: *hotspotPW,
		caStore:             *caStore,
		configDir:           *configDir,
		bridgeTokPath:       *bridgeTok,
		firstRunPath:        filepath.Join(*stateDir, "credentials", "first-run.flag"),
		deviceID:            id.DeviceID,
		dev:                 *devMode,
	}
	if *devMode {
		devPhc, err := crypto.HashPassword("devpass")
		if err != nil {
			log.Error("portal: dev hash", "err", err)
			os.Exit(1)
		}
		dp := os.TempDir() + "/portal-admin-dev"
		_ = os.WriteFile(dp, []byte(devPhc), 0o600)
		a.adminHashPath = dp
		if *listen == defaultListen {
			*listen = "127.0.0.1:8080"
		}
		log.Warn("portal: DEV MODE: password=devpass, HTTP", slog.String("listen", *listen))
	}
	if err := os.MkdirAll(*caStore, 0o750); err != nil {
		log.Warn("portal: cannot create ca-store", "err", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /login", http.HandlerFunc(a.getLogin))
	mux.Handle("POST /login", http.HandlerFunc(a.postLogin))
	mux.Handle("POST /logout", http.HandlerFunc(a.postLogout))

	authed := http.NewServeMux()
	authed.Handle("GET /first-run", http.HandlerFunc(a.getFirstRun))
	authed.Handle("POST /first-run", http.HandlerFunc(a.postFirstRun))
	authed.Handle("GET /", http.HandlerFunc(a.getHome))
	authed.Handle("GET /wifi", http.HandlerFunc(a.getWifi))
	authed.Handle("POST /wifi/scan", http.HandlerFunc(a.postWifiScan))
	authed.Handle("POST /wifi/connect", http.HandlerFunc(a.postWifiConnect))
	authed.Handle("POST /wifi/hotspot", http.HandlerFunc(a.postWifiHotspot))
	authed.Handle("GET /bridge", http.HandlerFunc(a.getBridge))
	authed.Handle("POST /bridge", http.HandlerFunc(a.postBridge))
	authed.Handle("POST /discovery", http.HandlerFunc(a.postDiscovery))
	authed.Handle("POST /wifi/ca", http.HandlerFunc(a.postUploadCA))
	authed.Handle("GET /test-print", http.HandlerFunc(a.getTestPrint))
	authed.Handle("POST /test-print", http.HandlerFunc(a.postTestPrint))
	authed.Handle("GET /password", http.HandlerFunc(a.getPassword))
	authed.Handle("POST /password", http.HandlerFunc(a.postPassword))
	authed.Handle("POST /password/wifi", http.HandlerFunc(a.postWifiPassword))
	authed.Handle("GET /diag", http.HandlerFunc(a.getDiag))
	authed.Handle("POST /actions/reload-usb", http.HandlerFunc(a.postReloadUSB))
	authed.Handle("POST /actions/restart-bridge", http.HandlerFunc(a.postRestartBridge))
	authed.Handle("POST /actions/reboot", http.HandlerFunc(a.postReboot))

	mux.Handle("/", a.sess.RequireSession("/login",
		[]string{"/login", "/logout", "/static/logo.png", "/static/icon.png"},
		a.requireFirstRunDone(authed)))

	sem := make(chan struct{}, maxConnsConcurr)
	server := &http.Server{
		Addr:              *listen,
		Handler:           securityHeaders(sizeLimit(semaphored(sem, mux))),
		ReadHeaderTimeout: readHeaderTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		log.Info("portal: shutting down")
		shCtx, shCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shCancel()
		_ = server.Shutdown(shCtx)
	}()

	_ = stateDir

	if *devMode {
		server.Addr = *listen
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("portal: serve error", "err", err)
			os.Exit(1)
		}
		return
	}
	if _, err := os.Stat(*certFile); err != nil {
		log.Error("portal: TLS cert missing, run firstboot script first", "cert", *certFile)
		os.Exit(1)
	}

	go func() {
		captive := &http.Server{
			Addr:              ":80",
			Handler:           captivePortalHandler(panelHost()),
			ReadHeaderTimeout: readHeaderTimeout,
			WriteTimeout:      writeTimeout,
			IdleTimeout:       idleTimeout,
		}
		if err := captive.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Warn("portal: captive HTTP serve error", "err", err)
		}
	}()

	if err := server.ListenAndServeTLS(*certFile, *keyFile); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("portal: serve error", "err", err)
		os.Exit(1)
	}
}

// panelHost devolve o endereço estável do painel. O IP do hotspot morre quando
// o equipamento troca de rede; o nome printserver-<id>.local continua válido
// na rede nova e evita que o celular mostre "visite 10.42.0.1".
func panelHost() string {
	return panelHostFrom(readHostname())
}

func panelHostFrom(hostname string) string {
	host := strings.TrimSpace(hostname)
	if host == "" {
		return "printserver.local"
	}
	host = strings.TrimSuffix(host, ".")
	if strings.HasSuffix(strings.ToLower(host), ".local") {
		return host
	}
	return host + ".local"
}

func captivePortalHandler(host string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		portalURL := "https://" + host + "/"
		switch r.URL.Path {
		case "/generate_204",
			"/gen_204",
			"/hotspot-detect.html",
			"/library/test/success.html",
			"/success.txt",
			"/ncsi.txt",
			"/connecttest.txt",
			"/redirect",
			"/check_network_status.txt",
			"/":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="pt-BR">
<head>
<meta charset="utf-8">
<meta http-equiv="refresh" content="0;url=` + portalURL + `">
<title>PrintServer</title>
</head>
<body>
<p>Abrindo o painel... <a href="` + portalURL + `">clique aqui</a> se nao abrir.</p>
</body>
</html>`))
			return
		}
		http.Redirect(w, r, portalURL, http.StatusFound)
	})
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (a *app) firstRunPending() bool {
	_, err := os.Stat(a.firstRunPath)
	return err == nil
}

func (a *app) requireFirstRunDone(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first-run" || r.URL.Path == "/logout" {
			next.ServeHTTP(w, r)
			return
		}
		if a.firstRunPending() {
			http.Redirect(w, r, "/first-run", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy",
			"default-src 'self'; "+
				"style-src 'self' 'unsafe-inline'; "+
				"font-src 'self'; "+
				"img-src 'self' data:; "+
				"script-src 'self' 'unsafe-inline'; "+
				"connect-src 'self'; "+
				"frame-ancestors 'none'; "+
				"base-uri 'self'; "+
				"form-action 'self'")
		h.Set("Permissions-Policy",
			"camera=(), microphone=(), geolocation=(), interest-cohort=()")
		next.ServeHTTP(w, r)
	})
}

func sizeLimit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost &&
			(r.URL.Path == "/wifi/ca" || r.URL.Path == "/wifi/connect") {
			next.ServeHTTP(w, r)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
		next.ServeHTTP(w, r)
	})
}

func semaphored(sem chan struct{}, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case sem <- struct{}{}:
			defer func() { <-sem }()
			next.ServeHTTP(w, r)
		default:
			http.Error(w, "server busy", http.StatusServiceUnavailable)
		}
	})
}

type pageData struct {
	Title          string
	Authenticated  bool
	ShowBack       bool
	ShowInitialPW  bool
	CSRF           string
	DeviceID       string
	ShortID        string
	Version        string
	Error          string
	Message        string
	NearbyNetworks []string
	NetStatus      netStatus
	Printers       []config.Printer
	Checks         []diagCheck
	Mode           string
	Hostname       string
	ReturnHost     string
	BridgeOpen     bool
	BridgeToken    string
	DiscoveryOn    bool
	NearbyWifi     []wifiNetwork
	ScannedAt      string
}

type netStatus struct {
	Mode          string
	Ident         string
	IP            string
	Internet      bool
	ManagerOK     bool
	PrintersCount int
	Ok            bool
	Offline       bool
	Label         string
	Detail        string
	Level         string
	AtTime        string
}

func statusSummary(hasIP, hasPrinter, hasInternet bool) (label, detail, level string) {
	switch {
	case !hasIP && !hasPrinter:
		return "Sem rede nem impressora", "Ligue o cabo ou configure o WiFi e conecte uma impressora USB.", "err"
	case !hasIP:
		return "Sem rede", "Ligue o cabo ou configure o WiFi.", "err"
	case !hasPrinter:
		return "Sem impressora", "Ligue uma impressora USB e use Reconhecer USB.", "warn"
	case !hasInternet:
		return "Pronto", "Sem internet. A impressão local continua funcionando.", "warn"
	default:
		return "Pronto", "", "ok"
	}
}

type diagCheck struct {
	Label  string
	Detail string
	Status string
}

func safeReturnHost(r *http.Request) string {
	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "printserver.local"
	}
	u, err := url.Parse("https://" + host)
	if err != nil || u.Host != host || u.User != nil || u.Path != "" ||
		strings.ContainsAny(host, "/?#@\\") {
		return "printserver.local"
	}
	return host
}

func (a *app) render(w http.ResponseWriter, page string, d pageData) {
	d.DeviceID = a.deviceID
	d.ShortID = strings.TrimPrefix(a.deviceID, "PS-")
	if len(d.ShortID) > 4 {
		d.ShortID = d.ShortID[len(d.ShortID)-4:]
	}
	d.Version = version.Version
	d.ShowBack = d.Authenticated && page != "home" && page != "first_run"
	tpl, err := template.ParseFS(tplFS, "templates/base.html", "templates/"+page+".html")
	if err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
		a.log.Error("portal: parseFS", "err", err, "page", page)
		return
	}
	if err := tpl.ExecuteTemplate(w, "base", d); err != nil {
		a.log.Error("portal: execute", "err", err, "page", page)
	}
}

func (a *app) csrfFail(w http.ResponseWriter, r *http.Request, back string) {
	a.log.Warn("portal: csrf fail", logger.ClientCIDR(mustAddr(r.RemoteAddr)))
	w.WriteHeader(http.StatusForbidden)
	a.render(w, "error", pageData{
		Title:   "Sessão expirada",
		Message: "A página ficou aberta tempo demais ou o navegador bloqueou o cookie de segurança. Aperte o botão abaixo para recarregar e tentar de novo.",
		Error:   back,
	})
}

func (a *app) configError(w http.ResponseWriter, err error) {
	a.log.Error("portal: config missing", "err", err)
	a.render(w, "error", pageData{
		Title:   "Configuração incompleta",
		Message: "O aparelho ainda não terminou a configuração inicial. Reinicie o aparelho e tente de novo.",
		Error:   "/login",
	})
}

func (a *app) getLogin(w http.ResponseWriter, r *http.Request) {
	csrf, err := auth.IssueCSRF(w, r)
	if err != nil {
		http.Error(w, "csrf", http.StatusInternalServerError)
		return
	}
	a.render(w, "login", pageData{Title: "Entrar", CSRF: csrf, ShowInitialPW: a.firstRunPending()})
}

func (a *app) postLogin(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if a.loginLimit.Blocked(auth.HTTPKey(r)) {
		csrf, _ := auth.IssueCSRF(w, r)
		a.render(w, "login", pageData{Title: "Entrar", CSRF: csrf, Error: "Muitas tentativas. Aguarde 1 minuto.", ShowInitialPW: a.firstRunPending()})
		return
	}
	pw := validate.Password(r.FormValue("password"))
	if pw == "" {
		csrf, _ := auth.IssueCSRF(w, r)
		a.render(w, "login", pageData{Title: "Entrar", CSRF: csrf, Error: "Informe a senha.", ShowInitialPW: a.firstRunPending()})
		return
	}
	phc, err := os.ReadFile(a.adminHashPath)
	if err != nil {
		a.configError(w, err)
		return
	}
	ok, _ := crypto.VerifyPassword(pw, strings.TrimSpace(string(phc)))
	if !ok {
		a.loginLimit.Record(auth.HTTPKey(r))
		a.log.Warn("portal: login failed", logger.ClientCIDR(mustAddr(r.RemoteAddr)))
		csrf, _ := auth.IssueCSRF(w, r)
		a.render(w, "login", pageData{Title: "Entrar", CSRF: csrf, Error: "Senha incorreta.", ShowInitialPW: a.firstRunPending()})
		return
	}
	if err := a.sess.Issue(w); err != nil {
		http.Error(w, "sess", http.StatusInternalServerError)
		return
	}
	a.log.Info("portal: login ok", logger.ClientCIDR(mustAddr(r.RemoteAddr)))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) postLogout(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	_ = a.sess.RotateKey()
	a.sess.Clear(w)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) getHome(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	st := a.collectStatus()
	cfg, _ := config.LoadPrinters(filepath.Join(a.configDir, "printers.json"))
	var printers []config.Printer
	if cfg != nil {
		for _, p := range cfg.Printers {
			if p.Enabled {
				printers = append(printers, p)
			}
		}
	}
	a.render(w, "home", pageData{
		Title: "Painel", Authenticated: true, CSRF: csrf,
		NetStatus: st, Printers: printers, BridgeOpen: readBridgeOpen(),
	})
}

func (a *app) getWifi(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	var msg string
	if r.URL.Query().Get("saved") == "wifi" {
		msg = "Senha do WiFi salva. Reconecte com a nova senha."
	}
	a.render(w, "wifi", pageData{
		Title:         "WiFi",
		Authenticated: true,
		CSRF:          csrf,
		Message:       msg,
		NearbyWifi:    scanWifiFull(),
		ScannedAt:     wifiScannedAt(),
		Mode:          readMode(),
		Hostname:      readHostname(),
		ReturnHost:    panelHost(),
	})
}

func (a *app) postWifiScan(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "wifi_scanning", pageData{
		Title:         "Procurando redes",
		Authenticated: true,
		CSRF:          csrf,
		Hostname:      readHostname(),
		ReturnHost:    panelHost(),
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		a.netMu.Lock()
		defer a.netMu.Unlock()
		time.Sleep(wifiSwitchDelay)
		if _, err := helperclient.Send("", "wifi-scan"); err != nil {
			a.log.Warn("portal: wifi-scan failed", "err", err)
		}
	}()
}

func (a *app) postWifiHotspot(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "wifi", pageData{
		Title:         "WiFi",
		Authenticated: true,
		CSRF:          csrf,
		Message:       "Voltando para o hotspot. O painel ficará indisponível por alguns segundos.",
		NearbyWifi:    scanWifiFull(),
		ScannedAt:     wifiScannedAt(),
		Mode:          readMode(),
		Hostname:      readHostname(),
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		a.netMu.Lock()
		defer a.netMu.Unlock()
		time.Sleep(wifiSwitchDelay)
		if _, err := helperclient.Send("", "mode-hotspot"); err != nil {
			a.log.Warn("portal: mode-hotspot failed", "err", err)
		}
	}()
}

func readMode() string {
	b, err := os.ReadFile("/etc/printserver/mode")
	if err != nil {
		return "hotspot"
	}
	s := strings.TrimSpace(string(b))
	if s == "client" {
		return "client"
	}
	return "hotspot"
}

func readHostname() string {
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "printserver"
}

// readBridgeOpen diz se qualquer máquina da rede pode imprimir. O modo
// protegido é opt-in: só existe quando o arquivo bridge-token-mode foi criado.
func readBridgeOpen() bool {
	return bridgeOpenFrom(bridgeTokenModePath)
}

func bridgeOpenFrom(markerPath string) bool {
	_, err := os.Stat(markerPath)
	return err != nil
}

// readBridgeToken devolve o segredo compartilhado da impressão, sem a quebra
// de linha escrita no primeiro boot. O valor aparece no painel para que o
// operador possa repassá-lo a quem configura o sistema que imprime.
func (a *app) readBridgeToken() string {
	raw, err := os.ReadFile(a.bridgeTokPath)
	if err != nil {
		a.log.Warn("portal: cannot read bridge token", "err", err)
		return ""
	}
	return string(bytes.TrimRight(raw, "\r\n "))
}

func (a *app) getBridge(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "bridge", pageData{
		Title:         "Modo da impressão",
		Authenticated: true,
		CSRF:          csrf,
		BridgeOpen:    readBridgeOpen(),
		BridgeToken:   a.readBridgeToken(),
		DiscoveryOn:   readDiscoveryOn(),
	})
}

func (a *app) postBridge(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	mode := r.FormValue("mode")
	var action, msg, errMsg string
	switch mode {
	case "open":
		action = "bridge-open"
		msg = "Impressão aberta. Qualquer máquina da rede pode imprimir."
	case "auth":
		action = "bridge-auth"
		msg = "Impressão protegida. Só quem tiver o token pode imprimir."
	default:
		errMsg = "Modo inválido."
	}
	if errMsg == "" {
		if _, err := helperclient.Send("", action); err != nil {
			a.log.Warn("portal: bridge mode switch failed", "err", err)
			errMsg = "Não foi possível alterar o modo agora."
		}
	}
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "bridge", pageData{
		Title:         "Modo da impressão",
		Authenticated: true,
		CSRF:          csrf,
		BridgeOpen:    readBridgeOpen(),
		BridgeToken:   a.readBridgeToken(),
		DiscoveryOn:   readDiscoveryOn(),
		Message:       msg,
		Error:         errMsg,
	})
}

// readDiscoveryOn devolve o estado do anúncio automático. Sem marcador a
// impressora aparece na rede, que é o comportamento padrão.
func readDiscoveryOn() bool {
	_, err := os.Stat(discoveryOffPath)
	return err != nil
}

func (a *app) postDiscovery(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}

	mode := r.FormValue("mode")
	var action, msg, errMsg string
	switch mode {
	case "on":
		action = "discovery-on"
		msg = "A impressora vai aparecer automaticamente na rede."
	case "off":
		action = "discovery-off"
		msg = "Descoberta automática desligada. Configure a impressora pelo endereço e porta 9100."
	default:
		errMsg = "Opção inválida."
	}
	if errMsg == "" {
		if _, err := helperclient.Send("", action); err != nil {
			a.log.Warn("portal: discovery switch failed", "err", err)
			errMsg = "Não foi possível alterar a descoberta agora."
		}
	}

	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "bridge", pageData{
		Title:         "Impressão",
		Authenticated: true,
		CSRF:          csrf,
		BridgeOpen:    readBridgeOpen(),
		BridgeToken:   a.readBridgeToken(),
		DiscoveryOn:   readDiscoveryOn(),
		Message:       msg,
		Error:         errMsg,
	})
}

func (a *app) wifiError(w http.ResponseWriter, r *http.Request, msg string) {
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "wifi", pageData{
		Title: "WiFi", Authenticated: true, CSRF: csrf, Error: msg,
		NearbyWifi: scanWifiFull(), ScannedAt: wifiScannedAt(),
		Hostname: readHostname(), Mode: readMode(),
	})
}

func (a *app) postWifiConnect(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCAUpload+maxRequestBody)
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		if err := r.ParseMultipartForm(maxCAUpload + maxRequestBody); err != nil {
			a.wifiError(w, r, "Arquivo muito grande. Use um certificado de até 32 KB.")
			return
		}
	}
	ssid := validate.SSID(r.FormValue("ssid"))
	if ssid == "" {
		a.wifiError(w, r, "Nome da rede inválido.")
		return
	}
	nt := r.FormValue("type")
	if nt == "enterprise" {
		if err := a.saveUploadedCA(r, ssid); err != nil {
			a.log.Warn("portal: CA upload failed", "err", err)
			a.wifiError(w, r, "Não foi possível salvar o certificado enviado.")
			return
		}
	}
	var prepErr error
	switch nt {
	case "open":
		prepErr = a.prepOpenProfile(ssid)
	case "personal":
		pw := validate.Password(r.FormValue("password"))
		if pw == "" || len(pw) < 8 {
			a.wifiError(w, r, "A senha da rede precisa ter no mínimo 8 caracteres.")
			return
		}
		prepErr = a.prepPersonalProfile(ssid, pw)
	case "enterprise":
		user := validate.ADUser(r.FormValue("username"))
		pw := validate.Password(r.FormValue("enterprise_password"))
		if user == "" || pw == "" {
			a.wifiError(w, r, "Usuário e senha são obrigatórios.")
			return
		}
		validateCert := r.FormValue("validate_cert") == "on"
		prepErr = a.prepEnterpriseProfile(ssid, user, pw, validateCert)
	default:
		a.wifiError(w, r, "Tipo inválido.")
		return
	}
	if prepErr != nil {
		a.log.Warn("portal: nm profile prep failed", "err", prepErr)
		a.wifiError(w, r, "Não foi possível preparar a conexão. Tente novamente.")
		return
	}

	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "wifi_connecting", pageData{
		Title:         "Trocando para " + ssid,
		Authenticated: true,
		CSRF:          csrf,
		Message:       ssid,
		Hostname:      readHostname(),
		ReturnHost:    panelHost(),
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		a.netMu.Lock()
		defer a.netMu.Unlock()
		time.Sleep(wifiSwitchDelay)
		if _, err := helperclient.Send("", "mode-client"); err != nil {
			a.log.Warn("portal: switch to client mode failed", "err", err)
		}
	}()
}

func (a *app) getTestPrint(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	cfg, _ := config.LoadPrinters(filepath.Join(a.configDir, "printers.json"))
	var ps []config.Printer
	if cfg != nil {
		for _, p := range cfg.Printers {
			if p.Enabled {
				ps = append(ps, p)
			}
		}
	}
	a.render(w, "test_print", pageData{Title: "Teste de impressão", Authenticated: true, CSRF: csrf, Printers: ps})
}

func (a *app) postTestPrint(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	serial := r.FormValue("serial")
	cfg, _ := config.LoadPrinters(filepath.Join(a.configDir, "printers.json"))
	var target *config.Printer
	if cfg != nil {
		for i := range cfg.Printers {
			if cfg.Printers[i].SerialNumber == serial && cfg.Printers[i].Enabled {
				target = &cfg.Printers[i]
				break
			}
		}
	}
	csrf, _ := auth.IssueCSRF(w, r)
	var ps []config.Printer
	if cfg != nil {
		for _, p := range cfg.Printers {
			if p.Enabled {
				ps = append(ps, p)
			}
		}
	}
	if target == nil {
		a.render(w, "test_print", pageData{Title: "Teste de impressão", Authenticated: true, CSRF: csrf, Printers: ps, Error: "Impressora não encontrada."})
		return
	}
	if err := a.sendTestJob(*target); err != nil {
		a.log.Error("portal: test print failed", "err", err, slog.String("printer", target.Name))
		a.render(w, "test_print", pageData{Title: "Teste de impressão", Authenticated: true, CSRF: csrf, Printers: ps, Error: "Não foi possível imprimir: " + err.Error()})
		return
	}
	a.render(w, "test_print", pageData{Title: "Teste de impressão", Authenticated: true, CSRF: csrf, Printers: ps, Message: "Página enviada pra " + target.Name + "."})
}

func (a *app) sendTestJob(p config.Printer) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(p.TCPPort))
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	if !readBridgeOpen() {
		tokenRaw, err := os.ReadFile(a.bridgeTokPath)
		if err != nil {
			return fmt.Errorf("token: %w", err)
		}
		token := bytes.TrimRight(tokenRaw, "\r\n ")
		var h [60]byte
		copy(h[:4], []byte("CPJ1"))
		if _, err := rand.Read(h[4:20]); err != nil {
			return err
		}
		binary.BigEndian.PutUint64(h[20:28], uint64(time.Now().UnixMilli()))
		msg := append([]byte{}, h[:28]...)
		msg = append(msg, []byte(strconv.Itoa(p.TCPPort))...)
		mac := auth.RawHMAC(token, msg)
		copy(h[28:60], mac)
		if _, err := conn.Write(h[:]); err != nil {
			return err
		}
	}

	// Impressoras laser (ex.: RICOH SP 310) entendem PCL, não ESC/POS.
	// O texto puro mais um form feed funciona nos dois mundos: a página é
	// ejetada em vez de ficar presa no buffer esperando um fim de trabalho.
	payload := []byte("\x1bE")
	payload = append(payload, []byte("*** Teste PrintServer ***\n")...)
	payload = append(payload, []byte("PrintServer · "+a.deviceID+"\n")...)
	payload = append(payload, []byte(time.Now().Format("2006-01-02 15:04:05")+"\n\n")...)
	payload = append(payload, []byte("Impressora: "+p.Name+"\n")...)
	payload = append(payload, []byte("Porta TCP:  "+strconv.Itoa(p.TCPPort)+"\n\n\n")...)
	payload = append(payload, 0x0C, 0x1B, 'E')
	if _, err := conn.Write(payload); err != nil {
		return err
	}
	// Avisa o bridge que o trabalho terminou, mas mantém a leitura aberta
	// para receber a confirmação real da escrita no dispositivo USB.
	if tcp, ok := conn.(*net.TCPConn); ok {
		_ = tcp.CloseWrite()
	}
	_ = conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	var ack [1]byte
	if _, err := io.ReadFull(conn, ack[:]); err != nil {
		return fmt.Errorf("sem confirmação da impressora: %w", err)
	}
	if ack[0] != bridgeack.OK {
		msg := bridgeack.Message(ack[0])
		// O bridge envia o erro real logo após o byte de falha. Sem esse
		// detalhe não há como saber se foi a impressora, o cabo ou o USB.
		var detail [200]byte
		if n, err := conn.Read(detail[:]); err == nil && n > 0 {
			msg += ": " + strings.TrimSpace(string(detail[:n]))
		}
		return errors.New(msg)
	}
	return nil
}

func (a *app) getPassword(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "password", pageData{Title: "Trocar senha", Authenticated: true, CSRF: csrf})
}

func (a *app) writeHotspotPassword(pw string) error {
	if strings.ContainsAny(pw, "\r\n") {
		return errors.New("senha com quebra de linha")
	}
	tmp := a.hotspotPasswordPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(pw+"\n"), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, a.hotspotPasswordPath)
}

func (a *app) saveHotspotPage(w http.ResponseWriter, r *http.Request, msg string) {
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "password_wifi_saved", pageData{
		Title:         "Senha salva",
		Authenticated: true,
		CSRF:          csrf,
		Message:       msg,
		ReturnHost:    panelHost(),
	})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		a.netMu.Lock()
		defer a.netMu.Unlock()
		time.Sleep(wifiSwitchDelay)
		if _, err := helperclient.Send("", "reload-hotspot"); err != nil {
			a.log.Warn("portal: hotspot reload failed", "err", err)
		}
		if _, err := helperclient.Send("", "restart-hotspot"); err != nil {
			a.log.Warn("portal: hotspot restart failed", "err", err)
		}
	}()
}

func (a *app) postWifiPassword(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	cur := validate.Password(r.FormValue("current"))
	n1 := validate.Password(r.FormValue("new"))
	n2 := validate.Password(r.FormValue("confirm"))
	csrf, _ := auth.IssueCSRF(w, r)
	renderErr := func(msg string) {
		a.render(w, "password", pageData{Title: "Trocar senha", Authenticated: true, CSRF: csrf, Error: msg})
	}
	old, err := os.ReadFile(a.hotspotPasswordPath)
	if err != nil || cur != strings.TrimSpace(string(old)) {
		renderErr("Senha atual do WiFi incorreta.")
		return
	}
	if len(n1) < 8 {
		renderErr("A nova senha do WiFi precisa ter no mínimo 8 caracteres.")
		return
	}
	if n1 != n2 {
		renderErr("As duas senhas novas não conferem.")
		return
	}
	if n1 == cur {
		renderErr("Escolha uma senha diferente da atual.")
		return
	}
	if err := a.writeHotspotPassword(n1); err != nil {
		a.log.Error("portal: hotspot password write failed", "err", err)
		renderErr("Não foi possível salvar a senha do WiFi.")
		return
	}
	a.log.Info("portal: hotspot password changed", logger.ClientCIDR(mustAddr(r.RemoteAddr)))
	a.saveHotspotPage(w, r, "Senha do WiFi salva")
}

func (a *app) postPassword(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	cur := validate.Password(r.FormValue("current"))
	n1 := validate.Password(r.FormValue("new"))
	n2 := validate.Password(r.FormValue("confirm"))
	csrf, _ := auth.IssueCSRF(w, r)
	phc, err := os.ReadFile(a.adminHashPath)
	if err != nil {
		a.configError(w, err)
		return
	}
	ok, _ := crypto.VerifyPassword(cur, strings.TrimSpace(string(phc)))
	if !ok {
		a.render(w, "password", pageData{Title: "Trocar senha", Authenticated: true, CSRF: csrf, Error: "Senha atual incorreta."})
		return
	}
	if n1 == "" || len(n1) < 8 {
		a.render(w, "password", pageData{Title: "Trocar senha", Authenticated: true, CSRF: csrf, Error: "A nova senha precisa ter no mínimo 8 caracteres."})
		return
	}
	if n1 != n2 {
		a.render(w, "password", pageData{Title: "Trocar senha", Authenticated: true, CSRF: csrf, Error: "As duas senhas novas não conferem."})
		return
	}
	newPhc, err := crypto.HashPassword(n1)
	if err != nil {
		http.Error(w, "hash", http.StatusInternalServerError)
		return
	}
	tmp := a.adminHashPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newPhc+"\n"), 0o600); err != nil {
		http.Error(w, "write", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, a.adminHashPath); err != nil {
		http.Error(w, "rename", http.StatusInternalServerError)
		return
	}
	a.log.Info("portal: password changed", logger.ClientCIDR(mustAddr(r.RemoteAddr)))
	_ = a.sess.RotateKey()
	a.sess.Clear(w)
	http.Redirect(w, r, "/login?pwchanged=1", http.StatusSeeOther)
}

func (a *app) getFirstRun(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	a.render(w, "first_run", pageData{
		Title:         "Primeiro acesso",
		Authenticated: true,
		CSRF:          csrf,
	})
}

func (a *app) postFirstRun(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	n1 := validate.Password(r.FormValue("new"))
	n2 := validate.Password(r.FormValue("confirm"))
	csrf, _ := auth.IssueCSRF(w, r)
	render := func(err string) {
		a.render(w, "first_run", pageData{
			Title: "Primeiro acesso", Authenticated: true, CSRF: csrf, Error: err,
		})
	}
	if len(n1) < 8 {
		render("A nova senha precisa ter no mínimo 8 caracteres.")
		return
	}
	if n1 != n2 {
		render("As duas senhas não conferem.")
		return
	}
	if n1 == initialAdminPassword {
		render("A nova senha não pode ser \"" + initialAdminPassword + "\". Escolha outra.")
		return
	}
	newPhc, err := crypto.HashPassword(n1)
	if err != nil {
		http.Error(w, "hash", http.StatusInternalServerError)
		return
	}
	tmp := a.adminHashPath + ".tmp"
	if err := os.WriteFile(tmp, []byte(newPhc+"\n"), 0o600); err != nil {
		http.Error(w, "write", http.StatusInternalServerError)
		return
	}
	if err := os.Rename(tmp, a.adminHashPath); err != nil {
		http.Error(w, "rename", http.StatusInternalServerError)
		return
	}
	if err := os.Remove(a.firstRunPath); err != nil && !os.IsNotExist(err) {
		a.log.Warn("portal: cannot remove first-run flag", "err", err)
	}
	a.log.Info("portal: first-run password set", logger.ClientCIDR(mustAddr(r.RemoteAddr)))
	// A senha inicial era conhecida: invalidar qualquer sessão aberta com ela.
	_ = a.sess.RotateKey()
	_ = a.sess.Issue(w)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) getDiag(w http.ResponseWriter, r *http.Request) {
	csrf, _ := auth.IssueCSRF(w, r)
	checks := a.runDiag()
	a.render(w, "diag", pageData{Title: "Diagnóstico", Authenticated: true, CSRF: csrf, Checks: checks})
}

func (a *app) runDiag() []diagCheck {
	out := []diagCheck{}
	st := a.collectStatus()
	if st.IP != "" && st.IP != "-" {
		out = append(out, diagCheck{"Conexão de rede", st.Mode + " · " + st.IP, "ok"})
	} else {
		out = append(out, diagCheck{"Conexão de rede", "sem endereço IP", "err"})
	}
	if testInternet() {
		out = append(out, diagCheck{"Internet", "alcança servidores externos", "ok"})
	} else {
		out = append(out, diagCheck{"Internet", "sem acesso externo (pode ser rede isolada)", "warn"})
	}
	// Consultar o estado não pode derrubar as portas 9100+: um SIGHUP aqui
	// reiniciava os listeners enquanto o operador só queria diagnosticar.
	if _, err := helperclient.Send("", "bridge-status"); err == nil {
		out = append(out, diagCheck{"Serviço de impressão", "respondendo", "ok"})
	} else {
		out = append(out, diagCheck{"Serviço de impressão", "sem resposta", "err"})
	}
	if cfg, err := config.LoadPrinters(filepath.Join(a.configDir, "printers.json")); err == nil && len(cfg.Printers) > 0 {
		count := 0
		for _, p := range cfg.Printers {
			if p.Enabled {
				count++
			}
		}
		out = append(out, diagCheck{"Impressoras", strconv.Itoa(count) + " ativa(s)", "ok"})
	} else {
		out = append(out, diagCheck{"Impressoras", "nenhuma detectada", "warn"})
	}
	if _, err := os.Stat(defaultTLSCert); err == nil {
		out = append(out, diagCheck{"Certificado do painel", "instalado", "ok"})
	} else {
		out = append(out, diagCheck{"Certificado do painel", "ausente", "err"})
	}
	return out
}

func testInternet() bool {
	conn, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (a *app) postReloadUSB(w http.ResponseWriter, r *http.Request) {
	a.helperCall(w, r, "reload-usb")
}
func (a *app) postRestartBridge(w http.ResponseWriter, r *http.Request) {
	a.helperCall(w, r, "restart-bridge")
}
func (a *app) postReboot(w http.ResponseWriter, r *http.Request) {
	a.helperCall(w, r, "reboot")
}

func (a *app) helperCall(w http.ResponseWriter, r *http.Request, action string) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	if !a.actionsLimit.Allow(auth.HTTPKey(r)) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
		return
	}
	if _, err := helperclient.Send("", action); err != nil {
		a.log.Error("portal: helper failed", slog.String("action", action), "err", err)
		http.Error(w, "ação falhou", http.StatusBadGateway)
		return
	}
	a.log.Info("portal: action ok", slog.String("action", action), logger.ClientCIDR(mustAddr(r.RemoteAddr)))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *app) collectStatus() netStatus {
	st := netStatus{Mode: "desconhecido", Ident: "", IP: "-"}
	out, err := exec.Command("/usr/bin/nmcli", "-t", "-f",
		"TYPE,DEVICE,STATE,CONNECTION", "device", "status").Output()
	if err == nil {
		type dev struct{ Type, Name, Conn string }
		var devs []dev
		for _, line := range strings.Split(string(out), "\n") {
			parts := strings.SplitN(line, ":", 4)
			if len(parts) != 4 || parts[2] != "connected" {
				continue
			}
			devs = append(devs, dev{Type: parts[0], Name: parts[1], Conn: parts[3]})
		}
		for _, d := range devs {
			if d.Type == "ethernet" {
				st.Mode = "Cabo (Ethernet)"
				st.Ident = d.Conn
				break
			}
		}
		if st.Mode == "desconhecido" {
			for _, d := range devs {
				if d.Type == "wifi" {
					st.Mode = "WiFi"
					st.Ident = d.Conn
					break
				}
			}
		}
	}
	if ipOut, err := exec.Command("/usr/bin/hostname", "-I").Output(); err == nil {
		for _, ip := range strings.Fields(string(ipOut)) {
			if strings.Contains(ip, ".") && !strings.HasPrefix(ip, "127.") {
				st.IP = ip
				break
			}
		}
	}
	if cfg, err := config.LoadPrinters(filepath.Join(a.configDir, "printers.json")); err == nil {
		for _, p := range cfg.Printers {
			if p.Enabled {
				st.PrintersCount++
			}
		}
	}
	st.Internet = testInternet()
	hasIP := st.IP != "-" && st.IP != ""
	hasPrinter := st.PrintersCount > 0
	st.Ok = hasIP && hasPrinter
	st.Offline = !hasIP
	st.Label, st.Detail, st.Level = statusSummary(hasIP, hasPrinter, st.Internet)
	if _, err := os.Stat("/var/lib/printserver/credentials/device-cert.pem"); err == nil {
		st.ManagerOK = true
	}
	st.AtTime = time.Now().Format("15:04")
	return st
}

func scanWifi() []string {
	n := scanWifiFull()
	out := make([]string, 0, len(n))
	for _, v := range n {
		out = append(out, v.SSID)
	}
	return out
}

type wifiNetwork struct {
	SSID     string `json:"ssid"`
	Signal   int    `json:"signal"`
	Security string `json:"security"`
	Bars     int    `json:"bars"`
	InUse    bool   `json:"in_use"`
	Channel  int    `json:"channel,omitempty"`
}

const wifiScanCache = "/run/printserver/wifi-scan.json"

func wifiScanCachePath() string {
	if p := os.Getenv("PRINTBOX_WIFI_SCAN_CACHE"); p != "" {
		return p
	}
	return wifiScanCache
}

type wifiScanFile struct {
	ScannedAt string        `json:"scanned_at"`
	Networks  []wifiNetwork `json:"networks"`
}

func scanWifiFull() []wifiNetwork {
	if nets := scanWifiLive(); len(nets) > 0 {
		return nets
	}
	nets, _ := scanWifiCached()
	return nets
}

func scanWifiCached() ([]wifiNetwork, string) {
	b, err := os.ReadFile(wifiScanCachePath())
	if err != nil {
		return nil, ""
	}
	var f wifiScanFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, ""
	}
	seen := map[string]int{}
	out := make([]wifiNetwork, 0, len(f.Networks))
	for _, n := range f.Networks {
		if n.SSID == "" {
			continue
		}
		n.Bars = signalBars(n.Signal)
		if i, ok := seen[n.SSID]; ok {
			if n.Signal > out[i].Signal {
				out[i] = n
			}
			continue
		}
		seen[n.SSID] = len(out)
		out = append(out, n)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Signal > out[j].Signal })
	return out, f.ScannedAt
}

func wifiScannedAt() string {
	_, ts := scanWifiCached()
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ""
	}
	return t.Local().Format("02/01 às 15:04")
}

func scanWifiLive() []wifiNetwork {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	_ = exec.CommandContext(ctx, "/usr/bin/nmcli", "device", "wifi", "rescan").Run()
	cmd := exec.CommandContext(ctx, "/usr/bin/nmcli",
		"-t", "-e", "no",
		"-f", "IN-USE,SSID,SIGNAL,SECURITY",
		"device", "wifi", "list")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var result []wifiNetwork
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 4 {
			continue
		}
		ssid := strings.TrimSpace(parts[1])
		if ssid == "" || ssid == "--" || seen[ssid] {
			continue
		}
		seen[ssid] = true
		sig, _ := strconv.Atoi(strings.TrimSpace(parts[2]))
		sec := strings.ToLower(strings.TrimSpace(parts[3]))
		net := wifiNetwork{
			SSID:   ssid,
			Signal: sig,
			InUse:  strings.TrimSpace(parts[0]) == "*",
			Bars:   signalBars(sig),
		}
		switch {
		case sec == "" || sec == "--":
			net.Security = "open"
		case strings.Contains(sec, "802.1x") || strings.Contains(sec, "eap"):
			net.Security = "enterprise"
		default:
			net.Security = "wpa"
		}
		result = append(result, net)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].InUse != result[j].InUse {
			return result[i].InUse
		}
		return result[i].Signal > result[j].Signal
	})
	return result
}

func signalBars(sig int) int {
	switch {
	case sig >= 75:
		return 4
	case sig >= 55:
		return 3
	case sig >= 35:
		return 2
	case sig > 0:
		return 1
	}
	return 0
}

func (a *app) prepOpenProfile(ssid string) error {
	if _, err := helperclient.Send("", "wifi-profile-open", "open", ssid); err != nil {
		return err
	}
	a.log.Info("portal: wifi profile prepared", slog.String("type", "open"), slog.String("ssid", ssid))
	return nil
}

func (a *app) saveUploadedCA(r *http.Request, ssid string) error {
	f, _, err := r.FormFile("ca_file")
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxCAUpload+1))
	if err != nil {
		return err
	}
	if len(buf) > maxCAUpload {
		return errors.New("certificado muito grande")
	}
	if !strings.Contains(string(buf), "BEGIN CERTIFICATE") {
		return errors.New("arquivo não é um certificado PEM")
	}
	return os.WriteFile(filepath.Join(a.caStore, safeFilename(ssid)+".pem"), buf, 0o644)
}

func (a *app) prepPersonalProfile(ssid, pw string) error {
	if _, err := helperclient.Send("", "wifi-profile-personal", "personal", ssid, pw); err != nil {
		return err
	}
	a.log.Info("portal: wifi profile prepared", slog.String("type", "personal"), slog.String("ssid", ssid))
	return nil
}

func (a *app) prepEnterpriseProfile(ssid, user, pw string, validateCert bool) error {
	validate := "0"
	ca := "none"
	if validateCert {
		validate = "1"
		if found := a.lookupCA(ssid); found != "" {
			ca = found
		}
	}
	if _, err := helperclient.Send("", "wifi-profile-enterprise",
		"enterprise", ssid, user, pw, validate, ca); err != nil {
		return err
	}
	a.log.Info("portal: wifi profile prepared", slog.String("type", "enterprise"), slog.String("ssid", ssid))
	return nil
}

func (a *app) lookupCA(ssid string) string {
	path := a.caStore + "/" + safeFilename(ssid) + ".pem"
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func safeFilename(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}

func (a *app) postUploadCA(w http.ResponseWriter, r *http.Request) {
	if err := auth.VerifyCSRF(r); err != nil {
		a.csrfFail(w, r, r.URL.Path)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxCAUpload)
	if err := r.ParseMultipartForm(maxCAUpload); err != nil {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	ssid := validate.SSID(r.FormValue("ssid"))
	if ssid == "" {
		http.Error(w, "ssid inválido", http.StatusBadRequest)
		return
	}
	f, _, err := r.FormFile("ca_file")
	if err != nil {
		http.Error(w, "arquivo ausente", http.StatusBadRequest)
		return
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, maxCAUpload))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	if !strings.Contains(string(buf), "BEGIN CERTIFICATE") {
		http.Error(w, "arquivo não é PEM certificate", http.StatusBadRequest)
		return
	}
	path := filepath.Join(a.caStore, safeFilename(ssid)+".pem")
	if err := os.WriteFile(path, buf, 0o644); err != nil {
		http.Error(w, "cannot save", http.StatusInternalServerError)
		return
	}
	a.log.Info("portal: CA uploaded", slog.String("ssid", ssid))
	http.Redirect(w, r, "/wifi", http.StatusSeeOther)
}

func mustAddr(addr string) net.Addr {
	host, port, _ := net.SplitHostPort(addr)
	if host == "" {
		host = addr
		port = "0"
	}
	p, _ := strconv.Atoi(port)
	return &net.TCPAddr{IP: net.ParseIP(host), Port: p}
}

var _ = url.Parse
