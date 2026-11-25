package logger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net"
	"os"
	"strings"
)

func New(w io.Writer, level slog.Level) *slog.Logger {
	h := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.MessageKey {
				a.Value = slog.StringValue(sanitize(a.Value.String()))
			}
			return a
		},
	})
	return slog.New(h)
}

func Default() *slog.Logger {
	return New(os.Stdout, slog.LevelInfo)
}

func sanitize(s string) string {
	if len(s) > 4096 {
		s = s[:4096] + "...(truncated)"
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n', r == '\r':
			b.WriteString("\\n")
		case r < 0x20 && r != '\t':
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func SafeString(key, val string) slog.Attr {
	return slog.String(key, sanitize(val))
}

func HashedSerial(serial string) slog.Attr {
	sum := sha256.Sum256([]byte(serial))
	return slog.String("printer_sh", hex.EncodeToString(sum[:4]))
}

func ClientCIDR(addr net.Addr) slog.Attr {
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return slog.String("client_net", "unknown")
	}
	if v4 := ip.To4(); v4 != nil {
		n := &net.IPNet{IP: v4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}
		return slog.String("client_net", n.String())
	}
	n := &net.IPNet{IP: ip.Mask(net.CIDRMask(48, 128)), Mask: net.CIDRMask(48, 128)}
	return slog.String("client_net", n.String())
}

func FromContext(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

func WithContext(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

type loggerKey struct{}
