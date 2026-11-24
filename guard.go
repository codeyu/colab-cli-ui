package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// The watchdog: a small Python process the app starts on the VM, which
// releases the runtime at a deadline or once it has been idle for a while.
// It runs on the VM itself, so it works while the app is closed. It releases
// the runtime the way google.colab.runtime.unassign() does, with a POST to
// $TBE_RUNTIME_ADDR/unassign. (The approach follows Colab Playground's
// watchdog, github.com/scarletkc/colab-playground.)

const guardDir = "/root/.colab-cli-ui"

// watchdogPy is the watchdog. Busy means a Jupyter kernel ran code since the
// last look, or the GPU, the network (a download) or the CPU (a heavy job
// outside the kernel) was in use. The whole-VM CPU alone is no measure:
// Colab's own services keep an idle 2-core VM around 10 %.
const watchdogPy = `import datetime, json, os, subprocess, time, urllib.request
BASE = "/root/.colab-cli-ui"
CFG, STATE, LOG = BASE + "/watchdog.json", BASE + "/watchdog-state.json", BASE + "/watchdog.log"
POLL = 30
CPU_BUSY, GPU_BUSY, NET_BUSY = 40.0, 5.0, 100 * 1024  # percent, percent, bytes/s
KERNELS = "http://127.0.0.1:8080/api/kernels"

def log(msg):
    try:
        with open(LOG, "a") as fh:
            fh.write(time.strftime("%Y-%m-%d %H:%M:%S ") + msg + "\n")
    except OSError:
        pass

def load():
    try:
        with open(CFG) as fh:
            return json.load(fh)
    except Exception:
        return {}

def cpu():
    with open("/proc/stat") as fh:
        v = [int(x) for x in fh.readline().split()[1:]]
    return sum(v), v[3] + (v[4] if len(v) > 4 else 0)

def net():
    n = 0
    with open("/proc/net/dev") as fh:
        for line in fh.readlines()[2:]:
            name, data = line.split(":", 1)
            if name.strip() != "lo":
                f = data.split()
                n += int(f[0]) + int(f[8])
    return n

def gpu():
    try:
        out = subprocess.run(["nvidia-smi", "--query-gpu=utilization.gpu", "--format=csv,noheader,nounits"],
                             capture_output=True, text=True, timeout=20).stdout
        vals = [float(x) for x in out.split() if x.strip()]
        return max(vals) if vals else None
    except Exception:
        return None

def kernels():
    """(any kernel busy, newest last_activity as epoch) or None."""
    try:
        with urllib.request.urlopen(KERNELS, timeout=10) as r:
            ks = json.load(r)
    except Exception:
        return None
    busy, newest = False, 0.0
    for k in ks:
        busy = busy or k.get("execution_state") == "busy"
        try:
            t = datetime.datetime.fromisoformat(k["last_activity"].replace("Z", "+00:00")).timestamp()
            newest = max(newest, t)
        except Exception:
            pass
    return busy, newest

def runtime_addr(cfg):
    addr = cfg.get("runtime_addr") or os.environ.get("TBE_RUNTIME_ADDR")
    if addr:
        return addr
    for pid in os.listdir("/proc"):
        if pid.isdigit():
            try:
                with open("/proc/%s/environ" % pid, "rb") as fh:
                    for item in fh.read().split(b"\0"):
                        if item.startswith(b"TBE_RUNTIME_ADDR="):
                            return item.split(b"=", 1)[1].decode()
            except OSError:
                pass
    return None

def release(cfg, reason):
    addr = runtime_addr(cfg)
    if not addr:
        log("cannot release (%s): TBE_RUNTIME_ADDR unknown" % reason)
        return
    for attempt in range(3):
        try:
            req = urllib.request.Request("http://%s/unassign" % addr, data=b"", method="POST")
            with urllib.request.urlopen(req, timeout=30) as r:
                log("released (%s): HTTP %d" % (reason, r.status))
                if r.status == 200:
                    return
        except Exception as e:
            log("release failed (%s, attempt %d): %s" % (reason, attempt + 1, e))
        time.sleep(10)

def main():
    log("started, pid %d" % os.getpid())
    last_busy = time.time()
    c0, n0, t0 = cpu(), net(), time.time()
    while True:
        time.sleep(POLL)
        cfg = load()
        if cfg.get("cancelled"):
            log("cancelled")
            return
        now = time.time()
        c1, n1 = cpu(), net()
        total, idle = c1[0] - c0[0], c1[1] - c0[1]
        cpu_pct = 100.0 * (total - idle) / total if total > 0 else 0.0
        net_bps = (n1 - n0) / max(1e-6, now - t0)
        g = gpu()
        k = kernels()
        kernel_busy = bool(k and (k[0] or k[1] > t0))
        c0, n0, t0 = c1, n1, now
        if kernel_busy or cpu_pct >= CPU_BUSY or net_bps >= NET_BUSY or (g is not None and g >= GPU_BUSY):
            last_busy = now
        try:
            tmp = STATE + ".tmp"
            with open(tmp, "w") as fh:
                json.dump({"pid": os.getpid(), "now": now, "last_busy": last_busy, "cpu": cpu_pct,
                           "net": net_bps, "gpu": g, "kernel_busy": kernel_busy}, fh)
            os.replace(tmp, STATE)
        except OSError:
            pass
        deadline, idle_s = cfg.get("deadline"), cfg.get("idle_s")
        if isinstance(deadline, (int, float)) and now >= deadline:
            release(cfg, "deadline")
            return
        if isinstance(idle_s, (int, float)) and idle_s > 0 and now - last_busy >= idle_s:
            release(cfg, "idle for %d min" % (idle_s // 60))
            return

main()
`

// guardScript configures the watchdog and starts it when it is not running;
// %s are a base64 of watchdogPy and a JSON object of the changes (numbers
// only), and it prints the state for GuardStatus. With no changes it only
// reports.
const guardScript = `import base64, json, os, subprocess, sys, time
BASE = "/root/.colab-cli-ui"
os.makedirs(BASE, exist_ok=True)
CFG, STATE = BASE + "/watchdog.json", BASE + "/watchdog-state.json"
change = json.loads(%s)
try:
    with open(CFG) as fh:
        cfg = json.load(fh)
except Exception:
    cfg = {}
def alive():
    try:
        with open(STATE) as fh:
            pid = json.load(fh)["pid"]
        with open("/proc/%%d/cmdline" %% pid, "rb") as fh:
            return b"watchdog.py" in fh.read()
    except Exception:
        return False
running = alive()
now = time.time()
if change:
    if "hours" in change:
        h = change["hours"]
        cfg["deadline"] = now + h * 3600 if h and h > 0 else None
    if "extend_hours" in change:
        base = cfg.get("deadline") or now
        cfg["deadline"] = max(base, now) + change["extend_hours"] * 3600
    if "idle_min" in change:
        m = change["idle_min"]
        cfg["idle_s"] = m * 60 if m and m > 0 else None
    cfg["cancelled"] = not (cfg.get("deadline") or cfg.get("idle_s"))
    cfg["runtime_addr"] = os.environ.get("TBE_RUNTIME_ADDR") or cfg.get("runtime_addr")
    with open(CFG + ".tmp", "w") as fh:
        json.dump(cfg, fh)
    os.replace(CFG + ".tmp", CFG)
    if not cfg["cancelled"] and not alive():
        with open(BASE + "/watchdog.py", "wb") as fh:
            fh.write(base64.b64decode(%s))
        try:
            os.remove(STATE)
        except OSError:
            pass
        p = subprocess.Popen([sys.executable, BASE + "/watchdog.py"], stdin=subprocess.DEVNULL,
                             stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, start_new_session=True)
        with open(STATE, "w") as fh:
            json.dump({"pid": p.pid}, fh)  # until its first sample
        running = True
try:
    with open(STATE) as fh:
        st = json.load(fh)
except Exception:
    st = {}
try:
    with open(BASE + "/watchdog.log") as fh:
        last = fh.read().strip().splitlines()[-1:]
except Exception:
    last = []
print("__COLAB_UI_GUARD__" + json.dumps({
    "running": bool(running and not cfg.get("cancelled")),
    "deadline": cfg.get("deadline") or 0, "idle_s": cfg.get("idle_s") or 0,
    "now": now, "last_busy": st.get("last_busy") or 0,
    "addr": bool(cfg.get("runtime_addr") or os.environ.get("TBE_RUNTIME_ADDR")),
    "log": last[0] if last else ""}))
`

// GuardStatus is the watchdog's state on a VM. Times are seconds on the VM's
// clock; VMNow lets the page count down without trusting either clock.
type GuardStatus struct {
	Running     bool    `json:"running"`
	Deadline    float64 `json:"deadline"` // 0: no deadline
	IdleSeconds float64 `json:"idleSeconds"`
	VMNow       float64 `json:"vmNow"`
	LastBusy    float64 `json:"lastBusy"` // 0: not measured yet
	CanRelease  bool    `json:"canRelease"`
	LastLog     string  `json:"lastLog"`
}

func (c *Colab) guard(ctx context.Context, name string, change map[string]float64) (GuardStatus, error) {
	if err := checkName(name); err != nil {
		return GuardStatus{}, err
	}
	if change == nil {
		change = map[string]float64{}
	}
	ch, _ := json.Marshal(change)
	code := fmt.Sprintf(guardScript, pyLiteral(string(ch)), pyLiteral(base64.StdEncoding.EncodeToString([]byte(watchdogPy))))
	out, err := c.runQuiet(ctx, internal(code), "exec", "-s", name)
	if err != nil {
		return GuardStatus{}, err
	}
	i := strings.Index(out, "__COLAB_UI_GUARD__")
	if i < 0 {
		return GuardStatus{}, fmt.Errorf("没有读到看门狗的状态：%s", tail(strings.TrimSpace(stripANSI(out)), 400))
	}
	line := out[i+len("__COLAB_UI_GUARD__"):]
	if j := strings.IndexByte(line, '\n'); j >= 0 {
		line = line[:j]
	}
	var raw struct {
		Running  bool    `json:"running"`
		Deadline float64 `json:"deadline"`
		IdleS    float64 `json:"idle_s"`
		Now      float64 `json:"now"`
		LastBusy float64 `json:"last_busy"`
		Addr     bool    `json:"addr"`
		Log      string  `json:"log"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), &raw); err != nil {
		return GuardStatus{}, err
	}
	st := GuardStatus{
		Running: raw.Running, Deadline: raw.Deadline, IdleSeconds: raw.IdleS, VMNow: raw.Now,
		LastBusy: raw.LastBusy, CanRelease: raw.Addr, LastLog: raw.Log,
	}
	if st.Running && !st.CanRelease {
		return st, errors.New("看门狗已启动，但找不到释放运行时用的地址（TBE_RUNTIME_ADDR），自动释放可能无效")
	}
	return st, nil
}

// GetGuard reports the watchdog's state.
func (c *Colab) GetGuard(ctx context.Context, name string) (GuardStatus, error) {
	return c.guard(ctx, name, nil)
}

// SetGuard sets how long the runtime may run from now (hours, 0 for no
// limit) and after how many idle minutes it is released (0 for never), and
// starts the watchdog when needed. Both 0 stops it.
func (c *Colab) SetGuard(ctx context.Context, name string, hours float64, idleMinutes int) (GuardStatus, error) {
	if hours < 0 || hours > 72 || idleMinutes < 0 || idleMinutes > 24*60 {
		return GuardStatus{}, errors.New("时间超出范围")
	}
	return c.guard(ctx, name, map[string]float64{"hours": hours, "idle_min": float64(idleMinutes)})
}

// ExtendGuard moves the deadline later by hours.
func (c *Colab) ExtendGuard(ctx context.Context, name string, hours float64) (GuardStatus, error) {
	if hours <= 0 || hours > 24 {
		return GuardStatus{}, errors.New("时间超出范围")
	}
	return c.guard(ctx, name, map[string]float64{"extend_hours": hours})
}
