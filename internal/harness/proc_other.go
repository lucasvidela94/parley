//go:build !unix

package harness

import "os/exec"

// killGroup keeps the default: a cancel kills the harness process only. Its
// children may outlive it; WaitDelay stops parley from waiting on them.
func killGroup(*exec.Cmd) {}
