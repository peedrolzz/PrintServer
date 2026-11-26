package main

import (
	"context"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/printserver/printserver/internal/version"
	"github.com/printserver/printserver/pkg/auth"
	"github.com/printserver/printserver/pkg/bridgeack"
	"github.com/printserver/printserver/pkg/config"
	"github.com/printserver/printserver/pkg/logger"
	"github.com/printserver/printserver/pkg/printer"
)

const (
	defaultConfig       = "/etc/printserver/printers.json"
	defaultTokenPath    = "/var/lib/printserver/credentials/bridge-token"
	bridgeTokenModePath = "/etc/printserver/bridge-token-mode"
	defaultBind         = "0.0.0.0"
	maxJob              = 512 * 1024
	jobTimeout          = 30 * time.Second
	handshakeTimeout    = 5 * time.Second
	// Um sistema de impressão pode enviar vários cupons em sequência. Dez por
	// minuto derrubaria trabalhos reais, então o padrão é um por segundo.
	maxPerIPPerMin    = 60
	maxPerIPPerMinCap = 600
	maxAckDetail      = 160
)

// bridgeTokenModeEnabled é verdadeiro somente quando o operador ativou
// explicitamente o modo protegido. Sem esse arquivo a impressão é aberta a
// qualquer máquina da rede, como uma impressora comum na porta 9100.
func bridgeTokenModeEnabled() bool {
	return bridgeTokenModeFrom(bridgeTokenModePath)
}

func bridgeTokenModeFrom(markerPath string) bool {
	_, err := os.Stat(markerPath)
	return err == nil
}

// loadBridgeAuth relê o marcador e o token. Usado no arranque e no SIGHUP
// para que uma troca de modo no painel não deixe o bridge com a política
// antiga até o próximo restart.
func loadBridgeAuth(log *slog.Logger, tokenPath string) ([]byte, bool) {
	requireToken := bridgeTokenModeEnabled()
	if !requireToken {
		return nil, false
	}
	tokenRaw, err := os.ReadFile(tokenPath)
	if err != nil {
		log.Error("bridge: cannot read token", "err", err)
		os.Exit(1)
	}
	token := trimEOL(tokenRaw)
	if len(token) < 32 {
		log.Error("bridge: token too short, require >=32 bytes")
		os.Exit(1)
	}
	return token, true
}

func main() {
	cfgPath := flag.String("config", envOr("PRINTBRIDGE_CONFIG", defaultConfig), "printers.json path")
	tokenPath := flag.String("token", envOr("PRINTBRIDGE_TOKEN", defaultTokenPath), "shared HMAC token file")
	bind := flag.String("bind", envOr("PRINTBRIDGE_BIND", defaultBind), "bind address")
	showVer := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVer {
		fmt.Println(version.String())
		return
	}

	log := logger.Default()
	log.Info("bridge starting",
		slog.String("version", version.Version),
		slog.String("bind", *bind),
		slog.String("config", *cfgPath),
	)

	token, requireToken := loadBridgeAuth(log, *tokenPath)

	cfg, err := config.LoadPrinters(*cfgPath)
	if err != nil {
		log.Error("bridge: invalid printers.json", "err", err)
		os.Exit(1)
	}

	rl := auth.NewLimiter(time.Minute, maxPerIPPerMinFromEnv())
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)

	for {
		listeners := startListeners(log, *bind, cfg, token, requireToken, rl)
		select {
		case <-ctx.Done():
			closeAll(listeners)
			log.Info("bridge: shut down cleanly")
			return
		case <-hup:
			log.Info("bridge: SIGHUP, reloading config")
			reloaded, err := config.LoadPrinters(*cfgPath)
			if err != nil {
				log.Error("bridge: reload failed, keeping previous", "err", err)
				continue
			}
			closeAll(listeners)
			cfg = reloaded
			token, requireToken = loadBridgeAuth(log, *tokenPath)
		}
	}
}

func startListeners(log *slog.Logger, bind string, cfg *config.Printers, token []byte, requireToken bool, rl *auth.Limiter) []net.Listener {
	var out []net.Listener
	for _, p := range cfg.Printers {
		if !p.Enabled {
			continue
		}
		pr := p
		dev := printer.New(pr.DevicePath, 4)
		addr := net.JoinHostPort(bind, strconv.Itoa(pr.TCPPort))
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			log.Error("bridge: listen failed", "err", err, "addr", addr)
			continue
		}
		out = append(out, ln)
		log.Info("bridge: listening",
			slog.String("printer", pr.Name),
			slog.Int("port", pr.TCPPort),
			logger.HashedSerial(pr.SerialNumber),
		)
		go serve(log, ln, dev, pr, token, requireToken, rl)
	}
	return out
}

func closeAll(ls []net.Listener) {
	for _, l := range ls {
		_ = l.Close()
	}
}

func serve(log *slog.Logger, ln net.Listener, dev *printer.Device, p config.Printer, token []byte, requireToken bool, rl *auth.Limiter) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(log, conn, dev, p, token, requireToken, rl)
	}
}

func handle(log *slog.Logger, conn net.Conn, dev *printer.Device, p config.Printer, token []byte, requireToken bool, rl *auth.Limiter) {
	defer conn.Close()
	host := auth.PeerKey(conn.RemoteAddr())
	if !rl.Allow(host) {
		log.Warn("bridge: rate limit", logger.ClientCIDR(conn.RemoteAddr()))
		return
	}
	if !requireToken {
		_ = conn.SetReadDeadline(time.Now().Add(jobTimeout))
	} else {
		_ = conn.SetReadDeadline(time.Now().Add(handshakeTimeout))

		var h [60]byte
		if _, err := io.ReadFull(conn, h[:]); err != nil {
			log.Warn("bridge: handshake read failed", "err", err, logger.ClientCIDR(conn.RemoteAddr()))
			return
		}
		if string(h[:4]) != "CPJ1" {
			log.Warn("bridge: bad magic", logger.ClientCIDR(conn.RemoteAddr()))
			return
		}
		ts := time.UnixMilli(int64(binary.BigEndian.Uint64(h[20:28])))
		if time.Since(ts) > time.Minute || ts.After(time.Now().Add(time.Minute)) {
			log.Warn("bridge: stale timestamp",
				slog.Time("ts", ts),
				logger.ClientCIDR(conn.RemoteAddr()),
			)
			return
		}

		msg := make([]byte, 0, 28+5)
		msg = append(msg, h[:28]...)
		msg = append(msg, []byte(strconv.Itoa(p.TCPPort))...)

		if !auth.VerifyRawHMAC(token, msg, h[28:60]) {
			// Nunca registrar o MAC recebido: com nonce/timestamp válidos ele
			// pode ser reutilizado para replay dentro da janela de um minuto.
			log.Warn("bridge: bad mac", logger.ClientCIDR(conn.RemoteAddr()))
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(jobTimeout))
	}
	start := time.Now()
	n, err := dev.WriteJob(conn, maxJob, jobTimeout)
	ack := bridgeack.FromError(err)
	if err != nil {
		if errors.Is(err, printer.ErrBusy) {
			log.Warn("bridge: queue full",
				slog.String("printer", p.Name),
				logger.ClientCIDR(conn.RemoteAddr()),
			)
		} else {
			log.Error("bridge: job error",
				"err", err,
				slog.String("printer", p.Name),
				logger.HashedSerial(p.SerialNumber),
			)
		}
	} else {
		log.Info("bridge: job ok",
			slog.String("printer", p.Name),
			slog.Int64("bytes", n),
			slog.Duration("duration", time.Since(start)),
			logger.HashedSerial(p.SerialNumber),
			logger.ClientCIDR(conn.RemoteAddr()),
		)
	}
	// Clientes comuns (porta 9100) não leem resposta: escrever bytes neles
	// falha sem efeito. O painel, sim, usa esses bytes para dizer a verdade.
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, _ = conn.Write([]byte{ack})
	if err != nil {
		msg := err.Error()
		if len(msg) > maxAckDetail {
			msg = msg[:maxAckDetail]
		}
		_, _ = conn.Write([]byte(msg))
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// maxPerIPPerMinFromEnv permite ajustar o limite sem mudar o código. Um valor
// inválido volta ao padrão; um valor muito alto é limitado para não transformar
// a porta de impressão em um canal livre de abuso.
func maxPerIPPerMinFromEnv() int {
	n, err := strconv.Atoi(envOr("PRINTBRIDGE_MAX_PER_MIN", ""))
	if err != nil || n <= 0 {
		return maxPerIPPerMin
	}
	if n > maxPerIPPerMinCap {
		return maxPerIPPerMinCap
	}
	return n
}

func trimEOL(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ') {
		b = b[:len(b)-1]
	}
	return b
}
