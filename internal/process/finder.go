package process

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// ProcessInfo represents a running CLI process with its PID and working directory.
type ProcessInfo struct {
	PID     int
	CWD     string
	Command string
}

// FindProcesses returns all matching processes with their CWDs.
func FindProcesses(names ...string) ([]ProcessInfo, error) {
	out, err := exec.Command("ps", "ax", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}

	var procs []ProcessInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		command := strings.Join(fields[1:], " ")
		if !matchesProcess(command, names) {
			continue
		}

		cwd := getCWD(pid)
		procs = append(procs, ProcessInfo{
			PID:     pid,
			CWD:     cwd,
			Command: command,
		})
	}

	return procs, nil
}

// FindClaudeProcesses returns all running claude processes with their CWDs.
func FindClaudeProcesses() ([]ProcessInfo, error) {
	return FindProcesses("claude")
}

// FindCodexProcesses returns all running codex processes with their CWDs.
func FindCodexProcesses() ([]ProcessInfo, error) {
	return FindProcesses("codex")
}

func matchesProcess(command string, names []string) bool {
	fields := strings.Fields(command)
	for _, field := range fields {
		cleaned := strings.Trim(field, "\"'")
		base := filepath.Base(cleaned)
		for _, name := range names {
			if base == name {
				return true
			}
		}
	}
	return false
}

// getCWD returns the current working directory of a process using lsof.
func getCWD(pid int) string {
	out, err := exec.Command("lsof", "-p", fmt.Sprintf("%d", pid), "-Fn").Output()
	if err != nil {
		return ""
	}
	// lsof -Fn outputs lines starting with 'n' for name, 'f' for fd, 'p' for pid
	// We want the 'cwd' file descriptor
	lines := strings.Split(string(out), "\n")
	foundCWD := false
	for _, line := range lines {
		if line == "fcwd" {
			foundCWD = true
			continue
		}
		if foundCWD && strings.HasPrefix(line, "n/") {
			return line[1:] // strip the 'n' prefix
		}
	}
	return ""
}

// FindRemoteControlProcesses returns all running "claude remote-control" processes.
func FindRemoteControlProcesses() ([]ProcessInfo, error) {
	out, err := exec.Command("ps", "ax", "-o", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}

	var procs []ProcessInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}

		command := strings.Join(fields[1:], " ")
		if !strings.Contains(command, "remote-control") {
			continue
		}
		// Ensure it's actually a claude command
		if !matchesProcess(command, []string{"claude"}) {
			continue
		}

		cwd := getCWD(pid)
		procs = append(procs, ProcessInfo{
			PID:     pid,
			CWD:     cwd,
			Command: command,
		})
	}

	return procs, nil
}

// GetCWDByPID returns the working directory for a given PID using lsof.
func GetCWDByPID(pid int) string {
	return getCWD(pid)
}

// IsAliveForCWD checks if any matching process is running with the given working directory.
func IsAliveForCWD(procs []ProcessInfo, cwd string) bool {
	for _, p := range procs {
		if p.CWD == cwd {
			return true
		}
	}
	return false
}
