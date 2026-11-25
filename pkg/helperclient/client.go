package helperclient

import (
	"encoding/json"
	"errors"
	"net"
	"time"
)

const DefaultSocket = "/run/printserver/helper.sock"

type Response struct {
	OK      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
}

type request struct {
	Action string   `json:"action"`
	Args   []string `json:"args,omitempty"`
}

func Send(socket, action string, args ...string) (Response, error) {
	if socket == "" {
		socket = DefaultSocket
	}
	conn, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(40 * time.Second))
	if err := json.NewEncoder(conn).Encode(request{Action: action, Args: args}); err != nil {
		return Response{}, err
	}
	var r Response
	if err := json.NewDecoder(conn).Decode(&r); err != nil {
		return Response{}, err
	}
	if !r.OK {
		return r, errors.New("helper: " + r.Message)
	}
	return r, nil
}
