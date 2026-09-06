//go:build unix

package instrument

import (
	"os/exec"
	"syscall"
)

// detach puts sclang in its own process group so a terminal signal aimed at
// ompool does not reach it, and so terminate can take scsynth down with it.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminate signals the whole process group: sclang and the scsynth it booted.
func terminate(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
}
