//go:build !windows

package spotify

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

func prepareProcess(cmd *exec.Cmd)                 { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
func containProcess(cmd *exec.Cmd) (func(), error) { return func() { _ = terminateProcess(cmd) }, nil }
func terminateProcess(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if errors.Is(err, syscall.ESRCH) {
		return os.ErrProcessDone
	}
	return err
}
