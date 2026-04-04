package daemon

import (
	"os"
	"syscall"

	"github.com/byronellis/ragtime/internal/mux"
	"github.com/byronellis/ragtime/internal/protocol"
)

// ShellSpecFromRequest converts a protocol request to a ShellSpec.
func ShellSpecFromRequest(req protocol.ShellNewRequest) mux.ShellSpec {
	return mux.ShellSpec{
		Name:    req.Name,
		Command: req.Command,
		CWD:     req.CWD,
		Env:     req.Env,
	}
}

// sigFromName converts a signal name string to an os.Signal.
func sigFromName(name string) os.Signal {
	switch name {
	case "SIGKILL", "kill":
		return syscall.SIGKILL
	case "SIGTERM", "term", "":
		return syscall.SIGTERM
	case "SIGINT", "int":
		return syscall.SIGINT
	case "SIGHUP", "hup":
		return syscall.SIGHUP
	default:
		return syscall.SIGTERM
	}
}
