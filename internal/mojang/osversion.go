package mojang

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// osVersionOnce caches the host OS version; resolving it can cost a subprocess
// and it cannot change while we run.
var osVersionOnce = sync.OnceValue(detectOSVersion)

// osVersion returns the host OS version in the form Mojang's os.version rules
// expect: the macOS or Windows product version, and the kernel release on
// Linux (where no official manifest uses os.version anyway).
func osVersion() string {
	return osVersionOnce()
}

func detectOSVersion() string {
	switch runtime.GOOS {
	case "darwin":
		// Rules match the product version ("14.5"), not the Darwin kernel
		// release that uname reports.
		if v := runCommand("sw_vers", "-productVersion"); v != "" {
			return v
		}
	case "windows":
		// "Microsoft Windows [Version 10.0.19045.4291]"
		if out := runCommand("cmd", "/c", "ver"); out != "" {
			if m := regexp.MustCompile(`(\d+\.\d+\.\d+)`).FindString(out); m != "" {
				return m
			}
		}
	default:
		if data, err := os.ReadFile("/proc/sys/kernel/osrelease"); err == nil {
			return strings.TrimSpace(string(data))
		}
		if v := runCommand("uname", "-r"); v != "" {
			return v
		}
	}
	return ""
}

// runCommand runs a short command and returns its trimmed stdout, or "" if it
// fails. Version detection must never block startup, hence the timeout.
func runCommand(name string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
