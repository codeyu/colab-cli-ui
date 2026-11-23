package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// First run: find uv, install the CLI with it, and sign in to Google through
// the CLI's copy-paste OAuth flow, which prints a URL and then reads the
// authorization code from stdin.

// SetupStatus is what the first-run guide checks.
type SetupStatus struct {
	UVPath string      `json:"uvPath"` // "" when uv is not found
	Env    Environment `json:"env"`
}

// Setup reports whether uv and colab are installed.
func (c *Colab) Setup(ctx context.Context) SetupStatus {
	uv, _ := resolveUV()
	return SetupStatus{UVPath: uv, Env: c.Environment(ctx)}
}

func exeName(n string) string {
	if runtime.GOOS == "windows" {
		return n + ".exe"
	}
	return n
}

// resolveUV finds uv on the PATH or where its installer puts it; the app's
// PATH predates an install made while it runs.
func resolveUV() (string, error) {
	if p, err := exec.LookPath("uv"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, p := range []string{
		filepath.Join(home, ".local", "bin", exeName("uv")),
		filepath.Join(home, ".cargo", "bin", exeName("uv")),
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("没有找到 uv")
}

// uvToolBin asks uv where it puts tools' executables.
func uvToolBin() string {
	uv, err := resolveUV()
	if err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, uv, "tool", "dir", "--bin")
	hideWindow(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// InstallCLI installs google-colab-cli with `uv tool install`, streaming
// uv's output.
func (c *Colab) InstallCLI(ctx context.Context, out *mygo.Channel[Chunk]) error {
	uv, err := resolveUV()
	if err != nil {
		return err
	}
	return c.streamCmd(ctx, "", out, func(ctx context.Context) (*exec.Cmd, string, error) {
		cmd := exec.CommandContext(ctx, uv, "tool", "install", "--upgrade", "google-colab-cli")
		cmd.Env = append(os.Environ(), "NO_COLOR=1")
		cmd.WaitDelay = 3 * time.Second
		hideWindow(cmd)
		return cmd, "uv tool install --upgrade google-colab-cli", nil
	})
}

// AuthState says whether the CLI has usable credentials.
type AuthState struct {
	LoggedIn bool    `json:"loggedIn"`
	Account  Account `json:"account"`
	// NeedsLogin is true when the CLI asked for a sign-in (oauth2) or found
	// no Application Default Credentials (adc).
	NeedsLogin bool   `json:"needsLogin"`
	Error      string `json:"error,omitempty"`
}

func needsLogin(out string) bool {
	o := strings.ToLower(out)
	for _, s := range []string{
		"to authorize colab-cli",
		"enter the authorization code",
		"defaultcredentialserror",
		"could not automatically determine credentials",
		"reauthentication is needed",
		"invalid_grant",
	} {
		if strings.Contains(o, s) {
			return true
		}
	}
	return false
}

// AuthStatus checks the credentials with `colab whoami`. It never starts a
// sign-in: with no input the CLI's prompt fails at once.
func (c *Colab) AuthStatus(ctx context.Context) AuthState {
	out, err := c.run(ctx, "", "whoami")
	if err == nil {
		return AuthState{LoggedIn: true, Account: parseWhoami(out)}
	}
	if needsLogin(out) {
		return AuthState{NeedsLogin: true}
	}
	return AuthState{Error: err.Error()}
}

func parseWhoami(out string) Account {
	var a Account
	for _, line := range strings.Split(stripANSI(out), "\n") {
		line = strings.TrimSpace(line)
		k, v, ok := strings.Cut(line, ":")
		switch {
		case strings.HasPrefix(line, "- "):
			a.Scopes = append(a.Scopes, strings.TrimPrefix(line, "- "))
		case ok && k == "Auth provider":
			a.Provider = strings.TrimSpace(v)
		case ok && k == "Email":
			a.Email = strings.TrimSpace(v)
		case ok && k == "Expires in":
			a.ExpiresIn = strings.TrimSpace(v)
		}
	}
	if a.Scopes == nil {
		a.Scopes = []string{}
	}
	return a
}

// syncBuffer collects a process's output and wakes waiters as it grows.
type syncBuffer struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	grew chan struct{}
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.buf.Write(p)
	select {
	case b.grew <- struct{}{}:
	default:
	}
	return n, err
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type loginProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	out    *syncBuffer
	done   chan error
	cancel context.CancelFunc
}

var (
	loginMu sync.Mutex
	login   *loginProc
	authURL = regexp.MustCompile(`https://accounts\.google\.com/\S+`)
)

func stopLogin() {
	if login != nil {
		login.cancel()
		login = nil
	}
}

// LoginStart starts the CLI's sign-in and returns the Google address to open.
// When the CLI turns out to be signed in already it returns "" and nothing
// more is needed.
func (c *Colab) LoginStart(ctx context.Context) (string, error) {
	loginMu.Lock()
	defer loginMu.Unlock()
	stopLogin()

	// The process outlives this call while the user signs in.
	pctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	cmd, _, err := c.command(pctx, "whoami")
	if err != nil {
		cancel()
		return "", err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return "", err
	}
	buf := &syncBuffer{grew: make(chan struct{}, 1)}
	cmd.Stdout, cmd.Stderr = buf, buf
	if err := cmd.Start(); err != nil {
		cancel()
		return "", err
	}
	p := &loginProc{cmd: cmd, stdin: stdin, out: buf, done: make(chan error, 1), cancel: cancel}
	go func() { p.done <- cmd.Wait() }()

	deadline := time.After(60 * time.Second)
	for {
		if u := authURL.FindString(buf.String()); u != "" {
			login = p
			return u, nil
		}
		select {
		case err := <-p.done:
			cancel()
			if err == nil {
				return "", nil // already signed in
			}
			return "", &CommandError{Code: 1, Output: redact(buf.String())}
		case <-buf.grew:
		case <-deadline:
			cancel()
			return "", errors.New("colab 没有给出登录地址")
		case <-ctx.Done():
			cancel()
			return "", ctx.Err()
		}
	}
}

// LoginFinish passes the authorization code Google showed to the CLI and
// returns the signed-in account.
func (c *Colab) LoginFinish(ctx context.Context, code string) (AuthState, error) {
	code = strings.TrimSpace(code)
	if code == "" || strings.ContainsAny(code, "\r\n") {
		return AuthState{}, errors.New("请粘贴 Google 页面上显示的授权码")
	}
	loginMu.Lock()
	p := login
	login = nil
	loginMu.Unlock()
	if p == nil {
		return AuthState{}, errors.New("登录已过期，请重新开始")
	}
	defer p.cancel()
	if _, err := io.WriteString(p.stdin, code+"\n"); err != nil {
		return AuthState{}, err
	}
	p.stdin.Close()
	select {
	case err := <-p.done:
		out := p.out.String()
		if err != nil {
			msg := "授权码无效或已过期，请重新登录"
			if !strings.Contains(strings.ToLower(out), "invalid_grant") {
				msg = tail(strings.TrimSpace(stripANSI(redact(out))), 600)
			}
			return AuthState{NeedsLogin: true}, errors.New(msg)
		}
		st := AuthState{LoggedIn: true, Account: parseWhoami(out)}
		if strings.Contains(out, "Could not save the OAuth token") {
			st.Error = "登录成功，但令牌没能存入系统凭据管理器，下次还需要重新登录"
		}
		return st, nil
	case <-time.After(90 * time.Second):
		return AuthState{}, errors.New("等待 colab 超时")
	case <-ctx.Done():
		return AuthState{}, ctx.Err()
	}
}

// LoginCancel abandons a sign-in in progress.
func (c *Colab) LoginCancel() {
	loginMu.Lock()
	defer loginMu.Unlock()
	stopLogin()
}
