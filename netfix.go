package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// On networks whose IPv6 does not get through, the CLI's HTTP calls stall
// until they fall back to IPv4 (a minute or more each) and colab exec fails
// outright: its WebSocket tries only the first address, IPv6, and times out
// ("socket is already closed"). A sitecustomize module on the CLI's
// PYTHONPATH makes Python try IPv4 addresses first; IPv6 ones still follow,
// so IPv6-only networks keep working.

const ipv4FirstPy = `# Written by Colab CLI UI: try IPv4 addresses before IPv6 ones.
import socket as _s
_orig = _s.getaddrinfo
def _ipv4_first(*args, **kwargs):
    return sorted(_orig(*args, **kwargs), key=lambda r: r[0] != _s.AF_INET)
_s.getaddrinfo = _ipv4_first
`

var (
	hookOnce sync.Once
	hookDir  string
)

// ipv4HookDir writes the module once and returns its directory, or "" when
// it cannot.
func ipv4HookDir() string {
	hookOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil {
			return
		}
		dir := filepath.Join(base, "colab-cli-ui", "pyhook")
		if os.MkdirAll(dir, 0o755) != nil {
			return
		}
		if os.WriteFile(filepath.Join(dir, "sitecustomize.py"), []byte(ipv4FirstPy), 0o644) != nil {
			return
		}
		hookDir = dir
	})
	return hookDir
}

// withIPv4First returns env with the hook's directory first on PYTHONPATH.
func withIPv4First(env []string) []string {
	return withPythonPath(env, ipv4HookDir())
}

// withPythonPath returns env with dir first on PYTHONPATH.
func withPythonPath(env []string, dir string) []string {
	if dir == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	set := false
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, "PYTHONPATH") {
			if v != "" {
				dir += string(os.PathListSeparator) + v
			}
			out = append(out, "PYTHONPATH="+dir)
			set = true
			continue
		}
		out = append(out, kv)
	}
	if !set {
		out = append(out, "PYTHONPATH="+dir)
	}
	return out
}

// cliEnv is the environment of the colab processes the app starts.
func (c *Colab) cliEnv(extra ...string) []string {
	env := append(os.Environ(), extra...)
	if c.settings().IPv4First {
		env = withIPv4First(env)
	}
	// An account's keyring backend (see accountEnv).
	for _, kv := range extra {
		if dir, ok := strings.CutPrefix(kv, "COLAB_UI_PYPATH="); ok {
			env = withPythonPath(env, dir)
		}
	}
	return env
}
