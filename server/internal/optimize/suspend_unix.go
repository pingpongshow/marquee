//go:build unix

package optimize

import (
	"errors"
	"os"
	"syscall"
)

// suspend pauses a process where it is (SIGSTOP); resume lets it carry on (SIGCONT).
func suspend(p *os.Process) error {
	if p == nil {
		return errors.New("no process")
	}
	return p.Signal(syscall.SIGSTOP)
}

func resume(p *os.Process) error {
	if p == nil {
		return errors.New("no process")
	}
	return p.Signal(syscall.SIGCONT)
}
