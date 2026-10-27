//go:build unix

package harness

import (
	"os/exec"
	"syscall"
)

// killGroup runs the harness in its own process group and makes a cancel kill
// the whole group. Harnesses spawn children (MCP servers, tool shells); killing
// only the parent leaves them running and holding stdout open.
func killGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
