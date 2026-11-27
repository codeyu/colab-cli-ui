package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// Colab drives the google-colab-cli `colab` command for the page.
type Colab struct {
	mu  sync.Mutex
	cfg *Settings
}

var sessionName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,62}$`)

func checkName(name string) error {
	if !sessionName.MatchString(name) {
		return fmt.Errorf("会话名称无效：%q（只能包含字母、数字、_ . -）", name)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Environment

// Environment describes the installed CLI and the signed-in account.
type Environment struct {
	OK      bool   `json:"ok"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Error   string `json:"error,omitempty"`
}

// Environment finds colab and reports its version.
func (c *Colab) Environment(ctx context.Context) Environment {
	bin, err := resolveColab(c.settings().ColabPath)
	if err != nil {
		return Environment{Error: err.Error()}
	}
	out, err := c.run(ctx, "", "version")
	if err != nil {
		return Environment{Path: bin, Error: err.Error()}
	}
	v := strings.TrimSpace(stripANSI(out))
	v = strings.TrimPrefix(v, "Version: ")
	return Environment{OK: true, Path: bin, Version: v}
}

// Account is who the CLI is signed in as.
type Account struct {
	Provider  string   `json:"provider"`
	Email     string   `json:"email"`
	ExpiresIn string   `json:"expiresIn"`
	Scopes    []string `json:"scopes"`
}

// Whoami asks the CLI which account its credentials belong to. The first call
// may open the browser for Google's consent page.
func (c *Colab) Whoami(ctx context.Context) (Account, error) {
	out, err := c.run(ctx, "", "whoami")
	if err != nil {
		return Account{}, err
	}
	return parseWhoami(out), nil
}

// ---------------------------------------------------------------------------
// Usage

// Usage is the account's compute-unit balance and what it may allocate.
type Usage struct {
	Balance     float64   `json:"balance"`
	RatePerHour float64   `json:"ratePerHour"`
	Assignments int       `json:"assignments"`
	FetchedAt   time.Time `json:"fetchedAt"`
	// Eligibility is what the server's ccu-info reported, the same lists
	// Colab's web UI greys accelerators out by. Known is false when the
	// reply had none (older CLI or server).
	Eligibility Eligibility `json:"eligibility"`
}

// Eligibility lists the accelerators the account may and may not allocate,
// as the server names them (T4, A100, V6E1, …).
type Eligibility struct {
	Known          bool     `json:"known"`
	EligibleGPUs   []string `json:"eligibleGpus"`
	IneligibleGPUs []string `json:"ineligibleGpus"`
	EligibleTPUs   []string `json:"eligibleTpus"`
	IneligibleTPUs []string `json:"ineligibleTpus"`
}

// parseCcuInfo finds the raw ccu-info reply in colab's debug log.
func parseCcuInfo(out string) (Eligibility, bool) {
	for _, line := range strings.Split(out, "\n") {
		i := strings.Index(line, `{"currentBalance"`)
		if i < 0 {
			continue
		}
		var raw struct {
			EligibleGPUs   []string `json:"eligibleGpus"`
			IneligibleGPUs []string `json:"ineligibleGpus"`
			EligibleTPUs   []string `json:"eligibleTpus"`
			IneligibleTPUs []string `json:"ineligibleTpus"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(line[i:])), &raw) != nil {
			continue
		}
		e := Eligibility{
			Known:          raw.EligibleGPUs != nil || raw.IneligibleGPUs != nil || raw.EligibleTPUs != nil || raw.IneligibleTPUs != nil,
			EligibleGPUs:   nonNil(raw.EligibleGPUs),
			IneligibleGPUs: nonNil(raw.IneligibleGPUs),
			EligibleTPUs:   nonNil(raw.EligibleTPUs),
			IneligibleTPUs: nonNil(raw.IneligibleTPUs),
		}
		return e, true
	}
	return Eligibility{EligibleGPUs: []string{}, IneligibleGPUs: []string{}, EligibleTPUs: []string{}, IneligibleTPUs: []string{}}, false
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

var (
	balanceRe = regexp.MustCompile(`Current balance:\s*([-\d.]+)`)
	rateRe    = regexp.MustCompile(`Usage rate:\s*([-\d.]+)`)
	assignRe  = regexp.MustCompile(`Active assignments:\s*(\d+)`)
)

// Usage returns the compute-unit balance and the hourly rate.
func (c *Colab) Usage(ctx context.Context) (Usage, error) {
	return c.usageOf(ctx, false)
}

func (c *Colab) usageOf(ctx context.Context, quiet bool) (Usage, error) {
	// The debug log carries the server's whole reply, eligibility included,
	// which `colab usage` itself does not print.
	out, err := c.runWith(ctx, "", quiet, "--logtostderr", "usage")
	if err != nil {
		return Usage{}, err
	}
	out = stripANSI(out)
	u := Usage{FetchedAt: time.Now()}
	u.Eligibility, _ = parseCcuInfo(out)
	if m := balanceRe.FindStringSubmatch(out); m != nil {
		u.Balance, _ = strconv.ParseFloat(m[1], 64)
	} else {
		return u, fmt.Errorf("无法解析 colab usage 的输出：%s", tail(out, 300))
	}
	if m := rateRe.FindStringSubmatch(out); m != nil {
		u.RatePerHour, _ = strconv.ParseFloat(m[1], 64)
	}
	if m := assignRe.FindStringSubmatch(out); m != nil {
		u.Assignments, _ = strconv.Atoi(m[1])
	}
	return u, nil
}

// ---------------------------------------------------------------------------
// Sessions

// Session is a Colab runtime: a VM with a Jupyter kernel.
type Session struct {
	Name          string `json:"name"`
	Endpoint      string `json:"endpoint"`
	Hardware      string `json:"hardware"`
	Shape         string `json:"shape"`
	Variant       string `json:"variant"`
	Status        string `json:"status"` // IDLE, BUSY (...), or "" when unknown
	Busy          bool   `json:"busy"`
	LastExecution string `json:"lastExecution,omitempty"`
	// Local is false for runtimes on the server that this machine has no
	// record of (shown by the CLI as "[?]").
	Local bool `json:"local"`
	// Started is when the session was created, in Unix milliseconds; 0
	// when unknown, as for a runtime created elsewhere.
	Started int64 `json:"started"`
}

var sessionLine = regexp.MustCompile(`^\[(.+?)\]\s+(\S+)\s+\|\s+Hardware:\s*([^|]+?)\s+\|\s+Shape:\s*([^|]+?)\s+\|\s+Variant:\s*([^|]+?)(?:\s+\|\s+Status:\s*(.+))?$`)

func parseSessions(out string) []Session {
	var list []Session
	for _, line := range strings.Split(stripANSI(out), "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.HasPrefix(line, "  Last Execution:") && len(list) > 0 {
			list[len(list)-1].LastExecution = strings.TrimSpace(strings.TrimPrefix(line, "  Last Execution:"))
			continue
		}
		m := sessionLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		s := Session{
			Name: m[1], Endpoint: m[2], Hardware: m[3], Shape: m[4], Variant: m[5],
			Status: strings.TrimSpace(m[6]), Local: m[1] != "?",
		}
		s.Busy = strings.HasPrefix(s.Status, "BUSY")
		list = append(list, s)
	}
	return list
}

// Sessions lists the runtimes: those this machine created, with their kernel
// status, and any others the server still has assigned to the account.
func (c *Colab) Sessions(ctx context.Context) ([]Session, error) {
	var (
		wg                sync.WaitGroup
		statusOut, srvOut string
		statusErr, srvErr error
	)
	wg.Add(2)
	go func() { defer wg.Done(); statusOut, statusErr = c.run(ctx, "", "status") }()
	go func() { defer wg.Done(); srvOut, srvErr = c.run(ctx, "", "sessions") }()
	wg.Wait()
	if statusErr != nil && srvErr != nil {
		return nil, statusErr
	}
	local := parseSessions(statusOut)
	server := parseSessions(srvOut)
	byEndpoint := map[string]bool{}
	for i, s := range local {
		byEndpoint[s.Endpoint] = true
		local[i].Started = sessionStarted(s.Name, s.Endpoint)
	}
	for _, s := range server {
		if !byEndpoint[s.Endpoint] {
			if s.Name == "?" {
				s.Name = "未知-" + shortEndpoint(s.Endpoint)
			}
			s.Local = false
			local = append(local, s)
		}
	}
	if local == nil {
		local = []Session{}
	}
	if statusErr == nil {
		c.dropForwardsExcept(local)
	}
	return local, nil
}

func shortEndpoint(e string) string {
	if len(e) > 8 {
		return e[len(e)-8:]
	}
	return e
}

// NewSessionOptions are the choices for a new runtime.
type NewSessionOptions struct {
	Name string `json:"name"`
	// Accelerator is "CPU", a GPU (T4, L4, G4, H100, A100) or a TPU (v5e1, v6e1).
	Accelerator string `json:"accelerator"`
	HighMem     bool   `json:"highMem"`
	// Retries is how many more times to try when Colab has no capacity or
	// the network fails, and RetryDelay the seconds between tries. Unset,
	// they are the settings' defaults.
	Retries    *int `json:"retries,omitempty"`
	RetryDelay *int `json:"retryDelay,omitempty"`
}

func acceleratorArgs(acc string, highMem bool) []string {
	var args []string
	switch strings.ToUpper(acc) {
	case "", "CPU", "NONE":
	case "V5E1", "V6E1":
		args = append(args, "--tpu", strings.ToLower(acc))
	default:
		args = append(args, "--gpu", strings.ToUpper(acc))
	}
	if highMem {
		args = append(args, "--high-mem")
	}
	return args
}

// CreateSession allocates a new runtime, streaming the CLI's progress.
func (c *Colab) CreateSession(ctx context.Context, opts NewSessionOptions, out *mygo.Channel[Chunk]) error {
	if err := checkName(opts.Name); err != nil {
		return err
	}
	// Pin the account: the checks and the clean-up below must not follow a
	// switch made meanwhile. Names are kept unique across accounts, as
	// commands on a session find its account by its name.
	acct := c.accountFor(ctx, nil)
	ctx = withAccount(ctx, acct)
	retries, delay := c.retryPlan(opts)
	if retries > 0 {
		// The CLI retries a 503 itself, three times over 70 s: with the
		// app retrying, one try is one request, and the wait the one asked.
		ctx = withEnv(ctx, "COLAB_CLI_ASSIGN_RETRIES=0")
	}
	return retryCreate(ctx, retries, time.Duration(delay)*time.Second,
		func() error { return c.createOnce(ctx, acct, opts, out) },
		func(msg string) { _ = out.Send(Chunk{Stream: "info", Text: "\n" + msg + "\n"}) })
}

// retryCreate runs attempt until it succeeds, fails for good, or has been
// retried retries times, waiting delay between tries.
func retryCreate(ctx context.Context, retries int, delay time.Duration, attempt func() error, note func(string)) error {
	for try := 0; ; try++ {
		err := attempt()
		if err == nil || try >= retries || !retryableCreate(err) || ctx.Err() != nil {
			if err != nil && try > 0 && ctx.Err() == nil {
				return fmt.Errorf("试了 %d 次都没有成功：%w", try+1, err)
			}
			return err
		}
		note(fmt.Sprintf("第 %d 次没有成功，%s后重试（还剩 %d 次）…", try+1, waitText(delay), retries-try))
		select {
		case <-ctx.Done():
			return errors.New("已取消")
		case <-time.After(delay):
		}
	}
}

// retryPlan is how many retries a create gets and the seconds between them.
func (c *Colab) retryPlan(opts NewSessionOptions) (int, int) {
	s := c.settings()
	retries, delay := s.CreateRetries, s.CreateRetryDelay
	if opts.Retries != nil {
		retries = *opts.Retries
	}
	if opts.RetryDelay != nil {
		delay = *opts.RetryDelay
	}
	return min(max(retries, 0), 50), min(max(delay, 5), 3600)
}

// retryableCreate tells failures that may pass (no capacity, the network)
// from ones that will not (no entitlement, signed out, a taken name).
func retryableCreate(err error) bool {
	msg := strings.ToLower(err.Error())
	for _, never := range []string{"rejected accelerator", "登录", "已有名为", "名称无效", "已取消", "entitlement", "quota"} {
		if strings.Contains(msg, strings.ToLower(never)) {
			return false
		}
	}
	for _, maybe := range []string{"暂时没有空闲", "拒绝分配", "allocation refused", "precondition failed", "412", "429", "500", "502", "503", "504",
		"timed out", "timeout", "temporarily", "connection", "socket", "unavailable", "reset by peer"} {
		if strings.Contains(msg, maybe) {
			return true
		}
	}
	return false
}

// createOnce is one try of CreateSession, cleaning up after a failure.
func (c *Colab) createOnce(ctx context.Context, acct string, opts NewSessionOptions, out *mygo.Channel[Chunk]) error {
	if owner := c.sessionOwner(opts.Name); owner != "" && owner != acct {
		return fmt.Errorf("另一个账号上已有名为 %s 的会话，请换一个名字", opts.Name)
	}
	// `colab new` with a name already in use overwrites its record, and the
	// runtime it named keeps billing without one.
	before, err := c.serverEndpoints(ctx)
	if err != nil {
		return err
	}
	for _, s := range before {
		if s.Name == opts.Name {
			return fmt.Errorf("已有名为 %s 的会话，请换一个名字", opts.Name)
		}
	}
	args := append([]string{"new", "-s", opts.Name}, acceleratorArgs(opts.Accelerator, opts.HighMem)...)
	err = c.stream(ctx, "", out, args...)
	if err == nil {
		return nil
	}
	err = plainCreateError(err, opts.Accelerator)
	// The request may have been granted anyway: stop what it allocated
	// (before any retry, so that one never leaves two).
	if did := c.reapAfterFailedNew(acct, opts.Name, before); did != "" {
		return fmt.Errorf("%w（%s）", err, did)
	}
	return err
}

// StopSession releases a runtime.
func (c *Colab) StopSession(ctx context.Context, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	closeForwardsOf(name)
	out, err := c.run(ctx, "", "stop", "-s", name)
	if err == nil && strings.Contains(out, "not found") {
		// `colab stop` of an unknown name says so and exits 0.
		return "", fmt.Errorf("会话 %s 不存在：它可能已被释放或被 Colab 回收", name)
	}
	return strings.TrimSpace(stripANSI(out)), err
}

// StopAll releases every runtime this machine created.
func (c *Colab) StopAll(ctx context.Context) (string, error) {
	tunnels.Lock()
	var names []string
	for name := range tunnels.links {
		names = append(names, name)
	}
	tunnels.Unlock()
	for _, n := range names {
		closeForwardsOf(n)
	}
	out, err := c.run(ctx, "", "stop", "--all")
	return strings.TrimSpace(stripANSI(out)), err
}

// RestartKernel restarts a runtime's Jupyter kernel, clearing its state.
func (c *Colab) RestartKernel(ctx context.Context, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	out, err := c.run(ctx, "", "restart-kernel", "-s", name)
	return strings.TrimSpace(stripANSI(out)), err
}

// SessionURL returns the address that opens the runtime in Colab's web UI.
func (c *Colab) SessionURL(ctx context.Context, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	out, err := c.run(ctx, "", "url", "-s", name)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(stripANSI(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http") {
			return line, nil
		}
	}
	return "", fmt.Errorf("colab url 没有返回地址：%s", tail(out, 300))
}

// OpenInBrowser opens the runtime in Colab's web UI.
func (c *Colab) OpenInBrowser(ctx context.Context, name string) error {
	u, err := c.SessionURL(ctx, name)
	if err != nil {
		return err
	}
	return mygo.Shell.OpenExternal(u)
}

// OpenExternal opens a web page in the browser.
func (c *Colab) OpenExternal(url string) error {
	if !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "http://127.0.0.1:") {
		return errors.New("只能打开 https 地址")
	}
	return mygo.Shell.OpenExternal(url)
}

// OpenTerminal runs an interactive colab command in a terminal window: "repl",
// "ssh", "auth" (credentials for GCP on the VM) or "drivemount".
func (c *Colab) OpenTerminal(name, kind string) error {
	if err := checkName(name); err != nil {
		return err
	}
	switch kind {
	case "repl", "ssh", "auth", "drivemount":
	default:
		return fmt.Errorf("未知的终端命令：%s", kind)
	}
	s := c.settings()
	bin, err := resolveColab(s.ColabPath)
	if err != nil {
		return err
	}
	tail := []string{kind, "-s", name}
	acct := c.accountFor(context.Background(), tail)
	args := append(c.globalArgs(acct, tail), tail...)
	return openTerminal(bin, args, "colab "+kind+" "+name, c.cliEnv(accountEnv(acct)...))
}

// ---------------------------------------------------------------------------
// Execution

// Exec runs Python code in the session's kernel, whose state persists between
// calls. With shell, code is a bash script instead. colab exec gives up after
// 30 s without output unless told not to, which a model that loads quietly
// would hit, so it waits as long as the code runs.
func (c *Colab) Exec(ctx context.Context, name, code string, shell bool, out *mygo.Channel[Chunk]) error {
	if err := checkName(name); err != nil {
		return err
	}
	if strings.TrimSpace(code) == "" {
		return errors.New("没有要执行的代码")
	}
	if shell {
		code = "%%bash\n" + code
	}
	if !strings.HasSuffix(code, "\n") {
		code += "\n"
	}
	start := time.Now()
	if err := c.stream(ctx, code, out, "exec", "--timeout", execIdleTimeout, "-s", name); err != nil {
		return execFailure(err)
	}
	return cellError(name, start, cellsOf(code))
}

// ExecFile runs a local .py script or .ipynb notebook in the session. A
// notebook's results are written next to it as <name>_output.ipynb.
func (c *Colab) ExecFile(ctx context.Context, name, file string, out *mygo.Channel[Chunk]) error {
	if err := checkName(name); err != nil {
		return err
	}
	if _, err := os.Stat(file); err != nil {
		return err
	}
	start := time.Now()
	if err := c.stream(ctx, "", out, "exec", "--timeout", execIdleTimeout, "-s", name, "-f", file); err != nil {
		return execFailure(err)
	}
	return cellError(name, start, cellsOf(fileCells(file)...))
}

// RunOptions are the choices of an ephemeral `colab run` job.
type RunOptions struct {
	Script      string   `json:"script"`
	Args        []string `json:"args"`
	Accelerator string   `json:"accelerator"`
	HighMem     bool     `json:"highMem"`
	Keep        bool     `json:"keep"`
	Name        string   `json:"name"`
}

// RunScript provisions a fresh VM, runs a local script on it and releases the
// VM afterwards (unless Keep).
func (c *Colab) RunScript(ctx context.Context, opts RunOptions, out *mygo.Channel[Chunk]) error {
	if _, err := os.Stat(opts.Script); err != nil {
		return err
	}
	args := append([]string{"run"}, acceleratorArgs(opts.Accelerator, opts.HighMem)...)
	if opts.Keep {
		args = append(args, "--keep")
	}
	if opts.Name != "" {
		if err := checkName(opts.Name); err != nil {
			return err
		}
		args = append(args, "-s", opts.Name)
	}
	args = append(args, opts.Script)
	args = append(args, opts.Args...)
	return c.stream(ctx, "", out, args...)
}

// Install installs Python packages on the VM, with uv when it can.
func (c *Colab) Install(ctx context.Context, name string, packages []string, requirements string, out *mygo.Channel[Chunk]) error {
	if err := checkName(name); err != nil {
		return err
	}
	args := []string{"install", "-s", name}
	if requirements != "" {
		args = append(args, "-r", requirements)
	} else {
		var pkgs []string
		for _, p := range packages {
			if p = strings.TrimSpace(p); p != "" {
				if strings.HasPrefix(p, "-") {
					return fmt.Errorf("无效的包名：%s", p)
				}
				pkgs = append(pkgs, p)
			}
		}
		if len(pkgs) == 0 {
			return errors.New("请输入要安装的包")
		}
		args = append(args, pkgs...)
	}
	start := time.Now()
	if err := c.stream(ctx, "", out, args...); err != nil {
		return err
	}
	return cellError(name, start, installResult)
}

// ---------------------------------------------------------------------------
// Resources

// GPU is one accelerator's state, from nvidia-smi.
type GPU struct {
	Name     string  `json:"name"`
	MemUsed  float64 `json:"memUsed"`  // MiB
	MemTotal float64 `json:"memTotal"` // MiB
	Util     float64 `json:"util"`     // percent
	Temp     float64 `json:"temp"`     // °C
	Power    float64 `json:"power"`    // W
}

// Stats is a snapshot of the VM's resources.
type Stats struct {
	CPU       float64   `json:"cpu"` // percent
	CPUCount  int       `json:"cpuCount"`
	RAMUsed   float64   `json:"ramUsed"` // bytes
	RAMTotal  float64   `json:"ramTotal"`
	DiskUsed  float64   `json:"diskUsed"`
	DiskTotal float64   `json:"diskTotal"`
	GPUs      []GPU     `json:"gpus"`
	Python    string    `json:"python"`
	Time      time.Time `json:"time"`
}

const statsMarker = "__COLAB_UI_STATS__"

const statsScript = `import json as _j, shutil as _sh, subprocess as _sp, os as _os, sys as _sys
_r = {"gpus": [], "python": _sys.version.split()[0], "cpuCount": _os.cpu_count() or 0}
try:
    import psutil as _ps
    _m = _ps.virtual_memory(); _r["ramUsed"] = _m.total - _m.available; _r["ramTotal"] = _m.total
    _r["cpu"] = _ps.cpu_percent(interval=0.3)
except Exception:
    pass
try:
    _d = _sh.disk_usage("/content"); _r["diskUsed"] = _d.used; _r["diskTotal"] = _d.total
except Exception:
    pass
try:
    _o = _sp.run(["nvidia-smi", "--query-gpu=name,memory.used,memory.total,utilization.gpu,temperature.gpu,power.draw", "--format=csv,noheader,nounits"], capture_output=True, text=True, timeout=8).stdout
    for _l in _o.strip().splitlines():
        _p = [x.strip() for x in _l.split(",")]
        def _f(x):
            try: return float(x)
            except Exception: return 0.0
        _r["gpus"].append({"name": _p[0], "memUsed": _f(_p[1]), "memTotal": _f(_p[2]), "util": _f(_p[3]), "temp": _f(_p[4]), "power": _f(_p[5]) if len(_p) > 5 else 0.0})
except Exception:
    pass
print("` + statsMarker + `" + _j.dumps(_r))
del _j, _sh, _sp, _os, _sys, _r
`

// Stats measures the VM's CPU, memory, disk and GPUs. It runs a short snippet
// in the kernel, so it waits while the kernel is busy.
func (c *Colab) Stats(ctx context.Context, name string) (Stats, error) {
	if err := checkName(name); err != nil {
		return Stats{}, err
	}
	out, err := c.run(ctx, internal(statsScript), "exec", "-s", name)
	if err != nil {
		return Stats{}, err
	}
	for _, line := range strings.Split(stripANSI(out), "\n") {
		if i := strings.Index(line, statsMarker); i >= 0 {
			var s Stats
			if err := json.Unmarshal([]byte(strings.TrimSpace(line[i+len(statsMarker):])), &s); err != nil {
				return Stats{}, err
			}
			if s.GPUs == nil {
				s.GPUs = []GPU{}
			}
			s.Time = time.Now()
			return s, nil
		}
	}
	return Stats{}, fmt.Errorf("没有读到资源信息：%s", tail(stripANSI(out), 400))
}

// ---------------------------------------------------------------------------
// Files

// FileEntry is a file or directory on the VM.
type FileEntry struct {
	Name  string `json:"name"`
	Path  string `json:"path"`
	IsDir bool   `json:"isDir"`
}

// ListFiles lists a directory on the VM. Paths are relative to the VM's root,
// as "content" for /content.
func (c *Colab) ListFiles(ctx context.Context, name, dir string) ([]FileEntry, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	dir = cleanRemote(dir)
	args := []string{"ls", "-s", name}
	if dir != "" {
		args = append(args, dir)
	} else {
		args = append(args, "/")
	}
	out, err := c.run(ctx, "", args...)
	if err != nil {
		return nil, err
	}
	entries := []FileEntry{}
	sc := bufio.NewScanner(strings.NewReader(stripANSI(out)))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.HasPrefix(line, "[colab]") {
			return nil, errors.New(strings.TrimSpace(strings.TrimPrefix(line, "[colab]")))
		}
		isDir := strings.HasSuffix(line, "/")
		n := strings.TrimSuffix(line, "/")
		entries = append(entries, FileEntry{Name: n, Path: path.Join(dir, n), IsDir: isDir})
	}
	return entries, nil
}

func cleanRemote(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean("/" + p)
	return strings.TrimPrefix(p, "/")
}

// Upload asks for local files and uploads them into a directory on the VM. It
// returns the remote paths written.
func (c *Colab) Upload(ctx context.Context, name, dir string) ([]string, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:   mygo.CallerWindow(ctx),
		Title:    "选择要上传的文件",
		Multiple: true,
	})
	if err != nil || len(files) == 0 {
		return []string{}, err
	}
	return c.UploadPaths(ctx, name, dir, files)
}

// UploadPaths uploads the given local files into a directory on the VM.
func (c *Colab) UploadPaths(ctx context.Context, name, dir string, files []string) ([]string, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	dir = cleanRemote(dir)
	done := []string{}
	for _, f := range files {
		remote := path.Join(dir, filepath.Base(f))
		if _, err := c.run(ctx, "", "upload", "-s", name, f, remote); err != nil {
			return done, err
		}
		done = append(done, remote)
	}
	return done, nil
}

// Download asks where to save a remote file and downloads it there. It
// returns the local path, or "" when canceled.
func (c *Colab) Download(ctx context.Context, name, remote string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	remote = cleanRemote(remote)
	local, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "保存文件",
		DefaultPath: path.Base(remote),
	})
	if err != nil || local == "" {
		return "", err
	}
	if _, err := c.run(ctx, "", "download", "-s", name, remote, local); err != nil {
		return "", err
	}
	return local, nil
}

// RemoveFile deletes a file on the VM.
func (c *Colab) RemoveFile(ctx context.Context, name, remote string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	remote = cleanRemote(remote)
	if remote == "" || remote == "content" {
		return "", errors.New("不能删除这个目录")
	}
	out, err := c.run(ctx, "", "rm", "-s", name, remote)
	return strings.TrimSpace(stripANSI(out)), err
}

// ShowInFolder reveals a local file in the file manager.
func (c *Colab) ShowInFolder(p string) { mygo.Shell.ShowItemInFolder(p) }

// PickFile asks for a local file. kind is "script" (.py, .ipynb),
// "requirements" (.txt) or "" for any file. It returns "" when canceled.
func (c *Colab) PickFile(ctx context.Context, kind string) (string, error) {
	opts := mygo.OpenDialogOptions{Parent: mygo.CallerWindow(ctx), Title: "选择文件"}
	switch kind {
	case "script":
		opts.Filters = []mygo.FileFilter{{Name: "Python / Notebook", Extensions: []string{"py", "ipynb"}}, {Name: "所有文件", Extensions: []string{"*"}}}
	case "py":
		opts.Filters = []mygo.FileFilter{{Name: "Python", Extensions: []string{"py"}}, {Name: "所有文件", Extensions: []string{"*"}}}
	case "requirements":
		opts.Filters = []mygo.FileFilter{{Name: "requirements", Extensions: []string{"txt"}}, {Name: "所有文件", Extensions: []string{"*"}}}
	}
	files, err := mygo.Dialog.Open(opts)
	if err != nil || len(files) == 0 {
		return "", err
	}
	return files[0], nil
}

// ---------------------------------------------------------------------------
// History

// HistoryEvent is one entry of a session's history log.
type HistoryEvent struct {
	Timestamp string            `json:"timestamp"`
	Type      string            `json:"type"`
	Summary   string            `json:"summary"`
	Code      string            `json:"code,omitempty"`
	Output    string            `json:"output,omitempty"`
	Fields    map[string]string `json:"fields,omitempty"`
}

// historyRoot replaces the CLI's history directory in tests.
var historyRoot string

func historyDir() string {
	if historyRoot != "" {
		return historyRoot
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "colab-cli", "history")
}

// HistorySessions lists the sessions with a history log, newest first.
func (c *Colab) HistorySessions() ([]string, error) {
	entries, err := os.ReadDir(historyDir())
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	type item struct {
		name string
		mod  time.Time
	}
	var items []item
	for _, e := range entries {
		if n, ok := strings.CutSuffix(e.Name(), ".jsonl"); ok {
			info, _ := e.Info()
			var mod time.Time
			if info != nil {
				mod = info.ModTime()
			}
			items = append(items, item{n, mod})
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].mod.After(items[j].mod) })
	names := make([]string, len(items))
	for i, it := range items {
		names[i] = it.name
	}
	return names, nil
}

// History reads a session's history log, newest last; limit 0 reads all.
func (c *Colab) History(name string, limit int) ([]HistoryEvent, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(historyDir(), name+".jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return []HistoryEvent{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	events := []HistoryEvent{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		var raw map[string]any
		if json.Unmarshal(sc.Bytes(), &raw) != nil {
			continue
		}
		if code, _ := raw["code"].(string); strings.Contains(code, "__COLAB_UI_") {
			continue // the app's own cells: resource samples, download polls
		}
		events = append(events, toHistoryEvent(raw))
	}
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}
	return events, sc.Err()
}

func toHistoryEvent(raw map[string]any) HistoryEvent {
	str := func(k string) string {
		if v, ok := raw[k].(string); ok {
			return v
		}
		return ""
	}
	e := HistoryEvent{Timestamp: str("timestamp"), Type: str("event_type"), Fields: map[string]string{}}
	for k, v := range raw {
		switch k {
		case "timestamp", "event_type", "code", "outputs":
			continue
		}
		switch v := v.(type) {
		case string:
			e.Fields[k] = v
		case float64, bool:
			e.Fields[k] = fmt.Sprint(v)
		}
	}
	switch e.Type {
	case "execution":
		e.Code = str("code")
		e.Summary = firstLine(e.Code)
		if outs, ok := raw["outputs"].([]any); ok {
			var b strings.Builder
			for _, o := range outs {
				m, _ := o.(map[string]any)
				if m == nil {
					continue
				}
				switch t := m["text"].(type) {
				case string:
					b.WriteString(t)
				case []any:
					for _, s := range t {
						b.WriteString(fmt.Sprint(s))
					}
				}
				if data, ok := m["data"].(map[string]any); ok {
					if t, ok := data["text/plain"].(string); ok {
						b.WriteString(t + "\n")
					}
				}
				if m["output_type"] == "error" {
					b.WriteString(fmt.Sprint(m["ename"], ": ", m["evalue"], "\n"))
				}
			}
			e.Output = tail(b.String(), 20000)
		}
	case "file_operation":
		e.Summary = strings.TrimSpace(str("op") + " " + firstNonEmpty(str("path"), str("remote"), str("local")))
	case "automation":
		e.Summary = str("op")
		// An install is code like any other and can run again; drivemount
		// needs a person to sign in, so the page only flags it.
		if str("op") == "install" {
			e.Code = str("code")
		}
	case "session_created":
		e.Summary = "创建会话 " + firstNonEmpty(str("accelerator"), "") + " " + str("machine_shape")
	case "session_terminated":
		e.Summary = "会话结束（" + str("reason") + "）"
	default:
		e.Summary = e.Type
	}
	return e
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i] + " …"
	}
	return s
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// ExportHistory asks where to save a session's history and exports it there
// with `colab log -o`: .ipynb, .md, .txt or .jsonl by the file's suffix.
func (c *Colab) ExportHistory(ctx context.Context, name string) (string, error) {
	if err := checkName(name); err != nil {
		return "", err
	}
	local, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent:      mygo.CallerWindow(ctx),
		Title:       "导出会话历史",
		DefaultPath: name + ".ipynb",
		Filters: []mygo.FileFilter{
			{Name: "Jupyter Notebook", Extensions: []string{"ipynb"}},
			{Name: "Markdown", Extensions: []string{"md"}},
			{Name: "Text", Extensions: []string{"txt"}},
			{Name: "JSON Lines", Extensions: []string{"jsonl"}},
		},
	})
	if err != nil || local == "" {
		return "", err
	}
	if _, err := c.run(ctx, "", "log", "-s", name, "-o", local); err != nil {
		return "", err
	}
	return local, nil
}

// plainCreateError says in a sentence why Colab refused a runtime, in place
// of the CLI's traceback.
func plainCreateError(err error, acc string) error {
	msg := err.Error()
	if acc == "" || strings.EqualFold(acc, "NONE") {
		acc = "CPU"
	}
	switch {
	case strings.Contains(msg, "/assign") && strings.Contains(msg, "Service Unavailable"):
		return fmt.Errorf("Colab 暂时没有空闲的 %s（503 Service Unavailable）", acc)
	case strings.Contains(msg, "Allocation refused"):
		return fmt.Errorf("Colab 拒绝分配 %s（412）：可能同时开的会话太多，或暂时没有资源", acc)
	}
	return err
}

// waitText is "30 秒" or "2 分钟".
func waitText(d time.Duration) string {
	if d >= time.Minute && d%time.Minute == 0 {
		return fmt.Sprintf(" %d 分钟", int(d/time.Minute))
	}
	return fmt.Sprintf(" %d 秒", int(d.Round(time.Second)/time.Second))
}
