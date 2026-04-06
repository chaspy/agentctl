package provider

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	ccusageCacheStaleMaxAge       = 24 * time.Hour
	ccusageWatcherRefreshInterval = 4 * time.Minute
	ccusageWatcherRetryInterval   = 30 * time.Second
	ccusageWatcherStartupWait     = 1500 * time.Millisecond
	ccusageWatcherPollInterval    = 100 * time.Millisecond
	ccusageWatcherCommand         = "__ccusage-watch"
	ccusageWatcherStartLockMaxAge = 30 * time.Second
)

// RunCCUsageWatcher keeps the ccusage cache warm in the background so rate
// readers only need to read the shared state file.
func RunCCUsageWatcher() error {
	if pid, ok := readCCUsageWatcherPID(); ok && pid != os.Getpid() {
		return nil
	}
	if err := writeCCUsageWatcherPID(os.Getpid()); err != nil {
		return err
	}
	defer removeCCUsageWatcherPID()

	for {
		block, ok := fetchCCUsageActiveBlock()
		if ok {
			writeCCUsageCache(block)
		}

		sleepFor := ccusageWatcherRefreshInterval
		if !ok {
			sleepFor = ccusageWatcherRetryInterval
		}
		time.Sleep(sleepFor)
	}
}

func ensureCCUsageWatcher() {
	if _, ok := readCCUsageWatcherPID(); ok {
		return
	}

	release, ok := acquireCCUsageWatcherStartLock()
	if !ok {
		return
	}
	defer release()

	if _, ok := readCCUsageWatcherPID(); ok {
		return
	}

	exe, err := os.Executable()
	if err != nil {
		return
	}
	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return
	}
	defer devNull.Close()

	cmd := exec.Command(exe, ccusageWatcherCommand)
	cmd.Env = append(os.Environ(), "AGENTCTL_CCUSAGE_WATCHER=1")
	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
}

func waitForFreshCCUsageCache(timeout time.Duration) (*ccusageBlock, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if block, ok := readCCUsageCache(); ok {
			return block, true
		}
		time.Sleep(ccusageWatcherPollInterval)
	}
	return nil, false
}

func acquireCCUsageWatcherStartLock() (func(), bool) {
	path, err := ccusageWatcherStartLockPath()
	if err != nil {
		return nil, false
	}

	if info, err := os.Stat(path); err == nil && time.Since(info.ModTime()) > ccusageWatcherStartLockMaxAge {
		_ = os.Remove(path)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, false
	}
	_ = f.Close()
	return func() { _ = os.Remove(path) }, true
}

func readCCUsageWatcherPID() (int, bool) {
	path, err := ccusageWatcherPIDPath()
	if err != nil {
		return 0, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		_ = os.Remove(path)
		return 0, false
	}
	if !processAlive(pid) {
		_ = os.Remove(path)
		return 0, false
	}
	return pid, true
}

func writeCCUsageWatcherPID(pid int) error {
	path, err := ccusageWatcherPIDPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

func removeCCUsageWatcherPID() {
	path, err := ccusageWatcherPIDPath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func ccusageWatcherPIDPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".agentctl", "ccusage-watcher.pid"), nil
}

func ccusageWatcherStartLockPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".agentctl", "ccusage-watcher.lock"), nil
}
