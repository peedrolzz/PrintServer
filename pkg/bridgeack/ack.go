// Package bridgeack define a conversa curta entre o bridge e o painel.
// O bridge envia um byte após gravar no USB e o painel transforma esse byte
// em uma frase que o operador entende, sem precisar de log dentro da Pi.
package bridgeack

import (
	"errors"
	"io/fs"
	"syscall"

	"github.com/printserver/printserver/pkg/printer"
)

const (
	OK byte = iota
	Failure
	QueueFull
	NoDevice
	Timeout
	Permission
	Denied
)

// FromError transforma o erro da escrita USB em um código conhecido pelo painel.
func FromError(err error) byte {
	switch {
	case err == nil:
		return OK
	case errors.Is(err, printer.ErrBusy):
		return QueueFull
	case errors.Is(err, printer.ErrNoDevice), errors.Is(err, fs.ErrNotExist):
		return NoDevice
	case errors.Is(err, printer.ErrTimeout):
		return Timeout
	case errors.Is(err, syscall.EPERM):
		return Denied
	case errors.Is(err, fs.ErrPermission):
		return Permission
	default:
		return Failure
	}
}

// Message devolve o texto curto mostrado na tela de teste.
func Message(code byte) string {
	switch code {
	case QueueFull:
		return "a impressora está ocupada, tente de novo"
	case NoDevice:
		return "o dispositivo USB da impressora não foi encontrado"
	case Timeout:
		return "a impressora não respondeu a tempo"
	case Permission:
		return "sem permissão no arquivo da impressora"
	case Denied:
		return "acesso ao USB bloqueado pelo serviço"
	default:
		return "a impressora não confirmou o trabalho"
	}
}
