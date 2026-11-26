package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strconv"
	"syscall"
	"time"

	"github.com/printserver/printserver/internal/version"
	"github.com/printserver/printserver/pkg/logger"
)

type Request struct {
	Action string   `json:"action"`
	Args   []string `json:"args,omitempty"`
}

type Response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

var allowed = map[string][]string{
	"reload-usb": {
		"/usr/bin/systemctl", "restart", "printserver-discover.service",
	},
	"restart-bridge": {
		"/usr/bin/systemctl", "restart", "printserver-bridge.service",
	},
	"reload-bridge-config": {
		"/usr/bin/systemctl", "kill", "-s", "HUP", "printserver-bridge.service",
	},
	"bridge-status": {
		"/usr/bin/systemctl", "is-active", "printserver-bridge.service",
	},
	"reload-hotspot": {
		"/usr/bin/systemctl", "restart", "printserver-network.service",
	},
	"restart-hotspot": {
		"/usr/bin/systemctl", "restart", "printserver-hotspot.service",
	},
	"mode-client": {
		"/usr/local/sbin/printserver-set-mode", "client",
	},
	"mode-hotspot": {
		"/usr/local/sbin/printserver-set-mode", "hotspot",
	},
	"bridge-open": {
		"/usr/local/sbin/printserver-set-bridge-mode", "open",
	},
	"bridge-auth": {
		"/usr/local/sbin/printserver-set-bridge-mode", "auth",
	},
	"reboot": {
		"/usr/bin/systemctl", "reboot",
	},
	"wifi-scan": {
		"/usr/bin/systemctl", "start", "printserver-wifi-scan.service",
	},
	"wifi-profile-open": {
		"/usr/local/sbin/printserver-wifi-profile",
	},
	"wifi-profile-personal": {
		"/usr/local/sbin/printserver-wifi-profile",
	},
	"wifi-profile-enterprise": {
		"/usr/local/sbin/printserver-wifi-profile",
	},
}

var allowedArgs = map[string]int{
	"wifi-profile-open":       2,
	"wifi-profile-personal":   3,
	"wifi-profile-enterprise": 6,
}

func main() {
	sock := flag.String("socket", "/run/printserver/helper.sock", "unix socket")
	group := flag.String("group", "printserver", "group that may connect")
	showVer := flag.Bool("version", false, "print version")
	flag.Parse()

	if *showVer {
		fmt.Println(version.String())
		return
	}

	log := logger.Default()
	log.Info("helper starting", slog.String("socket", *sock))

	if err := os.MkdirAll("/run/printserver", 0o755); err != nil {
		log.Error("mkdir /run/printserver", "err", err)
		os.Exit(1)
	}
	_ = os.Remove(*sock)

	ln, err := net.Listen("unix", *sock)
	if err != nil {
		log.Error("listen", "err", err)
		os.Exit(1)
	}
	gid, err := groupID(*group)
	if err != nil {
		log.Warn("cannot resolve group, defaulting to current gid", "err", err)
		gid = os.Getgid()
	}
	if err := os.Chmod(*sock, 0o660); err != nil {
		log.Warn("chmod socket", "err", err)
	}
	if err := os.Chown(*sock, 0, gid); err != nil {
		log.Warn("chown socket", "err", err)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sig
		_ = ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Info("accept error (exiting)", "err", err)
			return
		}
		go handle(log, conn)
	}
}

func groupID(name string) (int, error) {
	g, err := user.LookupGroup(name)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(g.Gid)
}

func handle(log *slog.Logger, conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	var req Request
	if err := json.NewDecoder(conn).Decode(&req); err != nil {
		respond(conn, Response{OK: false, Message: "bad json"})
		return
	}
	argv, ok := allowed[req.Action]
	if !ok {
		log.Warn("helper: unknown action", slog.String("action", req.Action))
		respond(conn, Response{OK: false, Message: "action not allowed"})
		return
	}
	maxArgs, takesArgs := allowedArgs[req.Action]
	if takesArgs {
		if len(req.Args) != maxArgs {
			log.Warn("helper: wrong argument count", slog.String("action", req.Action), slog.Int("got", len(req.Args)))
			respond(conn, Response{OK: false, Message: "wrong argument count"})
			return
		}
	} else if len(req.Args) != 0 {
		log.Warn("helper: unexpected arguments", slog.String("action", req.Action))
		respond(conn, Response{OK: false, Message: "arguments not allowed"})
		return
	}
	cmd := exec.Command(argv[0], append(argv[1:], req.Args...)...)
	cmd.Env = []string{"PATH=/usr/bin:/bin:/sbin:/usr/sbin", "LANG=C"}
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Error("helper: action failed", slog.String("action", req.Action), "err", err)
		respond(conn, Response{OK: false, Message: err.Error() + ": " + string(out)})
		return
	}
	log.Info("helper: action ok", slog.String("action", req.Action))
	respond(conn, Response{OK: true, Message: "accepted"})
}

func respond(conn net.Conn, r Response) {
	_ = json.NewEncoder(conn).Encode(r)
}
