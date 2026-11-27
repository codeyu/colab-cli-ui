package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
)

// Chunk is a piece of output streamed from a running colab command.
type Chunk struct {
	// Stream is "stdout", "stderr", "image" or "info".
	Stream string `json:"stream"`
	// Text is the output's text, which may contain ANSI escape sequences.
	Text string `json:"text,omitempty"`
	// Image is a data: URL of a plot or picture the code displayed.
	Image string `json:"image,omitempty"`
	// Path is where the CLI saved the image locally.
	Path string `json:"path,omitempty"`
}

// ActivityEntry describes one invocation of the colab CLI.
type ActivityEntry struct {
	ID       int64     `json:"id"`
	Command  string    `json:"command"`
	Started  time.Time `json:"started"`
	Duration float64   `json:"duration"` // seconds; 0 while running
	ExitCode int       `json:"exitCode"`
	Running  bool      `json:"running"`
	Output   string    `json:"output,omitempty"` // the tail of what it printed
}

// Activity is broadcast whenever a colab command starts or finishes.
var Activity = mygo.NewEvent[ActivityEntry]("activity")

var (
	activityMu  sync.Mutex
	activityID  int64
	ansiPattern = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07]*\x07|\x1b_G[^\x1b]*\x1b\\`)
	imageLine   = regexp.MustCompile(`^\s*\[Image saved to: (.+)\]\s*$`)
)

func nextActivityID() int64 {
	activityMu.Lock()
	defer activityMu.Unlock()
	activityID++
	return activityID
}

var secretLine = regexp.MustCompile(`(?i)authorization|bearer\s|request headers|access_token|refresh_token|"token"`)

// redact drops the lines of colab's debug log that carry credentials (the
// request headers' bearer token, token responses), so they never reach the
// page's command log or an error message.
func redact(s string) string {
	if !secretLine.MatchString(s) {
		return s
	}
	lines := strings.Split(s, "\n")
	kept := lines[:0]
	for _, l := range lines {
		if secretLine.MatchString(l) {
			kept = append(kept, "[已隐藏一行含凭据的调试日志]")
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

// stripANSI removes terminal escape sequences from s.
func stripANSI(s string) string { return ansiPattern.ReplaceAllString(s, "") }

// tail keeps the last n bytes of s, on a rune boundary.
func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[len(s)-n:]
	for len(s) > 0 && !utf8.RuneStart(s[0]) {
		s = s[1:]
	}
	return "…" + s
}

// CommandError is returned when colab exits with a non-zero status.
type CommandError struct {
	Code   int
	Output string
}

func (e *CommandError) Error() string {
	msg := strings.TrimSpace(stripANSI(e.Output))
	if msg == "" {
		return fmt.Sprintf("colab exited with status %d", e.Code)
	}
	return tail(msg, 2000)
}

// command builds an exec.Cmd for colab with the global flags of the settings.
func (c *Colab) command(ctx context.Context, args ...string) (*exec.Cmd, string, error) {
	s := c.settings()
	bin, err := resolveColab(s.ColabPath)
	if err != nil {
		return nil, "", err
	}
	acct := c.accountFor(ctx, args)
	full := c.globalArgs(acct, args)
	full = append(full, args...)
	cmd := exec.CommandContext(ctx, bin, full...)
	cmd.Env = c.cliEnv(append([]string{
		"PYTHONIOENCODING=utf-8",
		"PYTHONUTF8=1",
		"PYTHONUNBUFFERED=1",
		"COLUMNS=160",
		"TERM=xterm-256color",
	}, accountEnv(acct)...)...)
	cmd.WaitDelay = 3 * time.Second
	hideWindow(cmd)
	display := "colab " + quoteArgs(full)
	return cmd, display, nil
}

// globalArgs are the flags before the command: the sign-in method, the
// account's state file (unless args name one) and the OAuth client.
func (c *Colab) globalArgs(acct string, args []string) []string {
	s := c.settings()
	var full []string
	if s.Auth != "" && s.Auth != "oauth2" {
		full = append(full, "--auth", s.Auth)
	}
	if !containsArg(args, "--config") {
		full = append(full, c.configArgs(acct)...)
	}
	if s.OAuthConfig != "" {
		full = append(full, "-c", s.OAuthConfig)
	}
	return full
}

// signInLost tells the page the sign-in is gone, when it is the active
// account's: another account's lapsed sign-in shows in the account list.
func signInLost(ctx context.Context) {
	if id, _ := ctx.Value(accountKey{}).(string); id == "" || id == activeAccount() {
		AuthLost.Broadcast(true)
	}
}

func quoteArgs(args []string) string {
	out := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t\"'&|<>") {
			out[i] = `"` + strings.ReplaceAll(a, `"`, `\"`) + `"`
		} else {
			out[i] = a
		}
	}
	return strings.Join(out, " ")
}

// run runs colab to completion and returns what it printed (stdout and stderr
// together). A non-zero exit becomes a *CommandError carrying the output.
func (c *Colab) run(ctx context.Context, stdin string, args ...string) (string, error) {
	return c.runWith(ctx, stdin, false, args...)
}

// runQuiet is run for the app's own frequent calls (polling a download),
// which stay out of the command log.
func (c *Colab) runQuiet(ctx context.Context, stdin string, args ...string) (string, error) {
	return c.runWith(ctx, stdin, true, args...)
}

func (c *Colab) runWith(ctx context.Context, stdin string, quiet bool, args ...string) (string, error) {
	ctx = withAccount(ctx, c.accountFor(ctx, args))
	cmd, display, err := c.command(ctx, args...)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	entry := ActivityEntry{ID: nextActivityID(), Command: display, Started: time.Now(), Running: true}
	if !quiet {
		Activity.Broadcast(entry)
	}
	err = cmd.Run()
	out := redact(buf.String())
	if !quiet {
		c.finish(&entry, out, err)
	}
	if err != nil && needsLogin(out) {
		signInLost(ctx)
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return out, &CommandError{Code: exitErr.ExitCode(), Output: out}
		}
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, err
	}
	return out, nil
}

func (c *Colab) finish(entry *ActivityEntry, out string, err error) {
	entry.Running = false
	entry.Duration = time.Since(entry.Started).Seconds()
	entry.Output = tail(stripANSI(redact(out)), 4000)
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		entry.ExitCode = exitErr.ExitCode()
	default:
		entry.ExitCode = -1
		if entry.Output == "" {
			entry.Output = err.Error()
		}
	}
	Activity.Broadcast(*entry)
}

// stream runs colab and sends what it prints to out as it arrives. Lines the
// CLI prints for saved images become image chunks.
func (c *Colab) stream(ctx context.Context, stdin string, out *mygo.Channel[Chunk], args ...string) error {
	ctx = withAccount(ctx, c.accountFor(ctx, args))
	return c.streamCmd(ctx, stdin, out, func(ctx context.Context) (*exec.Cmd, string, error) {
		return c.command(ctx, args...)
	})
}

// streamCmd is stream for any program: build makes the command, bound to the
// context it is given, and the text to show for it.
func (c *Colab) streamCmd(ctx context.Context, stdin string, out *mygo.Channel[Chunk], build func(context.Context) (*exec.Cmd, string, error)) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd, display, err := build(ctx)
	if err != nil {
		return err
	}
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	entry := ActivityEntry{ID: nextActivityID(), Command: display, Started: time.Now(), Running: true}
	Activity.Broadcast(entry)
	_ = out.Send(Chunk{Stream: "info", Text: "$ " + display + "\n"})
	if err := cmd.Start(); err != nil {
		c.finish(&entry, "", err)
		return err
	}

	var (
		logMu sync.Mutex
		log   strings.Builder
		wg    sync.WaitGroup
	)
	send := func(ch Chunk) {
		if ch.Text != "" {
			logMu.Lock()
			if log.Len() < 1<<20 {
				log.WriteString(ch.Text)
			}
			logMu.Unlock()
		}
		if out.Send(ch) != nil {
			cancel() // the page stopped listening
		}
	}
	wg.Add(2)
	go func() { defer wg.Done(); pump(stdout, "stdout", send) }()
	go func() { defer wg.Done(); pump(stderr, "stderr", send) }()
	wg.Wait()
	err = cmd.Wait()
	logMu.Lock()
	text := log.String()
	logMu.Unlock()
	c.finish(&entry, text, err)
	if err != nil {
		if needsLogin(text) {
			signInLost(ctx)
			return errors.New("Colab CLI 的登录已失效，请重新登录")
		}
		if ctx.Err() != nil {
			return errors.New("已取消")
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &CommandError{Code: exitErr.ExitCode(), Output: lastLines(text, 15)}
		}
		return err
	}
	return nil
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// pump reads r and sends its text in pieces: complete lines as soon as they
// end, and a partial line once the program pauses, so progress shows live.
func pump(r io.Reader, stream string, send func(Chunk)) {
	data := make(chan []byte, 16)
	go func() {
		defer close(data)
		buf := make([]byte, 8192)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				b := make([]byte, n)
				copy(b, buf[:n])
				data <- b
			}
			if err != nil {
				return
			}
		}
	}()
	var pending []byte
	flushLines := func(all bool) {
		for {
			i := bytes.IndexByte(pending, '\n')
			if i < 0 {
				break
			}
			line := string(pending[:i+1])
			pending = pending[i+1:]
			emitLine(line, stream, send)
		}
		if all && len(pending) > 0 {
			// Keep an incomplete rune, or an image notice still arriving.
			cut := len(pending)
			for cut > 0 && !utf8.Valid(pending[:cut]) {
				cut--
			}
			if bytes.HasPrefix(pending, []byte("[Image saved")) {
				return
			}
			if cut > 0 {
				send(Chunk{Stream: stream, Text: string(pending[:cut])})
				pending = pending[cut:]
			}
		}
	}
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case b, ok := <-data:
			if !ok {
				flushLines(true)
				if len(pending) > 0 {
					send(Chunk{Stream: stream, Text: string(pending)})
				}
				return
			}
			pending = append(pending, b...)
			flushLines(false)
			if len(pending) > 0 {
				timer.Reset(120 * time.Millisecond)
			}
		case <-timer.C:
			flushLines(true)
		}
	}
}

func emitLine(line, stream string, send func(Chunk)) {
	if m := imageLine.FindStringSubmatch(stripANSI(line)); m != nil {
		path := strings.TrimSpace(m[1])
		if url, err := dataURL(path); err == nil {
			send(Chunk{Stream: "image", Image: url, Path: path})
			return
		}
	}
	send(Chunk{Stream: stream, Text: line})
}

func dataURL(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	typ := mime.TypeByExtension(strings.ToLower(filepath.Ext(path)))
	if typ == "" {
		typ = "image/png"
	}
	return "data:" + typ + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}

// resolveColab finds the colab executable: the configured path, the PATH,
// then where `uv tool install` puts it.
func resolveColab(configured string) (string, error) {
	if configured != "" {
		if _, err := os.Stat(configured); err == nil {
			return configured, nil
		}
		return "", fmt.Errorf("找不到 colab 可执行文件：%s", configured)
	}
	if p, err := exec.LookPath("colab"); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	candidates := []string{
		filepath.Join(home, ".local", "bin", "colab.exe"),
		filepath.Join(home, ".local", "bin", "colab"),
	}
	if bin := uvToolBin(); bin != "" {
		candidates = append(candidates, filepath.Join(bin, exeName("colab")))
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", errors.New("没有找到 colab 命令。请先运行 `uv tool install google-colab-cli`，或在设置中指定路径")
}

func containsArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}
