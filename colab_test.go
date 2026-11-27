package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseSessions(t *testing.T) {
	out := "[trainer] gpu-t4-s-abc | Hardware: T4 | Shape: Standard | Variant: GPU | Status: BUSY (train.py)\n" +
		"  Last Execution: train.py | Cell: 3 at 2026-11-23 10:00\n" +
		"[?] cpu-xyz123456 | Hardware: CPU | Shape: High-RAM | Variant: DEFAULT\n" +
		"[colab] something else\n"
	got := parseSessions(out)
	if len(got) != 2 {
		t.Fatalf("got %d sessions: %+v", len(got), got)
	}
	s := got[0]
	if s.Name != "trainer" || s.Endpoint != "gpu-t4-s-abc" || s.Hardware != "T4" || s.Shape != "Standard" ||
		s.Variant != "GPU" || !s.Busy || s.Status != "BUSY (train.py)" || !s.Local ||
		s.LastExecution != "train.py | Cell: 3 at 2026-11-23 10:00" {
		t.Errorf("first: %+v", s)
	}
	if got[1].Local || got[1].Shape != "High-RAM" || got[1].Status != "" {
		t.Errorf("second: %+v", got[1])
	}
}

func TestAcceleratorArgs(t *testing.T) {
	for acc, want := range map[string]string{
		"CPU": "", "T4": "--gpu T4", "a100": "--gpu A100", "v6e1": "--tpu v6e1", "V5E1": "--tpu v5e1",
	} {
		if got := strings.Join(acceleratorArgs(acc, false), " "); got != want {
			t.Errorf("%s: %q, want %q", acc, got, want)
		}
	}
}

func TestCleanRemote(t *testing.T) {
	for in, want := range map[string]string{
		"content": "content", "/content/a/../b": "content/b", "": "", "..": "", `content\x`: "content/x",
	} {
		if got := cleanRemote(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestPumpImagesAndPartialLines(t *testing.T) {
	dir := t.TempDir()
	img := filepath.Join(dir, "plot.png")
	os.WriteFile(img, []byte("\x89PNG"), 0o644)
	r, w := io.Pipe()
	var chunks []Chunk
	done := make(chan struct{})
	go func() { pump(r, "stdout", func(c Chunk) { chunks = append(chunks, c) }); close(done) }()
	io.WriteString(w, "hello\n\n[Image saved to: "+img+"]\n")
	io.WriteString(w, "progress 50%")
	time.Sleep(300 * time.Millisecond) // the partial line is flushed after a pause
	if n := len(chunks); n == 0 || chunks[n-1].Text != "progress 50%" {
		t.Errorf("partial line not flushed: %+v", chunks)
	}
	io.WriteString(w, "\rprogress 100%\n")
	w.Close()
	<-done
	var images, text int
	var all strings.Builder
	for _, c := range chunks {
		if c.Stream == "image" {
			images++
			if !strings.HasPrefix(c.Image, "data:image/png;base64,") {
				t.Errorf("image: %q", c.Image)
			}
		} else {
			text++
			all.WriteString(c.Text)
		}
	}
	if images != 1 || all.String() != "hello\n\nprogress 50%\rprogress 100%\n" {
		t.Errorf("images=%d text=%q", images, all.String())
	}
}

func TestParseCcuInfo(t *testing.T) {
	out := "2026-11-23 - colab_cli.client - DEBUG - Request Headers: {'Authorization': 'Bearer ya29.secret'}\n" +
		"2026-11-23 - colab_cli.client - DEBUG - Response Body: )]}'\n" +
		`{"currentBalance":199.03, "consumptionRateHourly":0, "assignmentsCount":0, "eligibleGpus":["G4", "A100", "L4", "T4"], "ineligibleGpus":["H100"], "eligibleTpus":["V6E1", "V5E1"]}` + "\n" +
		"Current balance: 199.03 compute units\n"
	e, ok := parseCcuInfo(out)
	if !ok || !e.Known || strings.Join(e.EligibleGPUs, ",") != "G4,A100,L4,T4" ||
		strings.Join(e.IneligibleGPUs, ",") != "H100" || strings.Join(e.EligibleTPUs, ",") != "V6E1,V5E1" ||
		e.IneligibleTPUs == nil {
		t.Errorf("%+v ok=%v", e, ok)
	}
	if r := redact(out); strings.Contains(r, "ya29") || !strings.Contains(r, "Current balance") {
		t.Errorf("redact: %q", r)
	}
}

func TestStripANSI(t *testing.T) {
	if got := stripANSI("\x1b[31mred\x1b[0m \x1b_Ga=T;xx\x1b\\ok"); got != "red ok" {
		t.Errorf("%q", got)
	}
}

func TestHFJobScripts(t *testing.T) {
	job := "/root/.colab-cli-ui/jobs/abc123"
	script := fmt.Sprintf(hfJobScript, pyLiteral(`Qwen/Qwen3-0.6B`), pyLiteral([]string{"model.safetensors", `we"ird<>.json`}), pyLiteral("/content/models/Qwen3-0.6B"), pyLiteral(job), 1234)
	scripts := map[string]string{
		"job.py":      script,
		"launch.py":   fmt.Sprintf(jobLauncher, pyLiteral(job), pyLiteral("cHJpbnQoMSk=")),
		"poll.py":     fmt.Sprintf(jobPoll, pyLiteral(job), 42),
		"kill.py":     fmt.Sprintf(jobKill, pyLiteral(job)),
		"watchdog.py": watchdogPy,
		"guard.py":    fmt.Sprintf(guardScript, pyLiteral(`{"hours": 2, "idle_min": 60}`), pyLiteral("cHJpbnQoMSk=")),
		"llm.py":      fmt.Sprintf(llmDeployScript, pyLiteral("hf.co/a/b:Q4_K_M"), pyLiteral(""), pyLiteral(job), 8192),
		"llmstat.py":  llmStatusScript,
		"llmrm.py":    fmt.Sprintf(llmRemoveScript, pyLiteral("qwen3:8b")),
	}
	for n, code := range scripts {
		if strings.Contains(code, "%!") {
			t.Fatalf("%s: format error:\n%s", n, code)
		}
		if dir := os.Getenv("HF_SCRIPT_OUT"); dir != "" {
			os.WriteFile(filepath.Join(dir, n), []byte(code), 0o644)
		}
	}
	for id, ok := range map[string]bool{"Qwen/Qwen3-0.6B": true, "gpt2": true, "a/b/c": false, "../x": false, "a b/c": false} {
		if hfRepoID.MatchString(id) != ok {
			t.Errorf("%q: want %v", id, ok)
		}
	}
}

func TestSplitPoll(t *testing.T) {
	out := "[colab] noise\n\r  1.00 / 2.00 GB 50%\n__COLAB_UI_POLL__{\"offset\": 120, \"state\": \"running\", \"error\": \"\", \"alive\": true}\n"
	text, st := splitPoll(out)
	if st == nil || st.Offset != 120 || st.State != "running" || !st.Alive {
		t.Fatalf("status = %+v", st)
	}
	if text != "[colab] noise\n\r  1.00 / 2.00 GB 50%" {
		t.Errorf("text = %q", text)
	}
	if _, st := splitPoll("Traceback ..."); st != nil {
		t.Error("parsed a poll without its status line")
	}
}

func TestWithIPv4First(t *testing.T) {
	dir := ipv4HookDir()
	if dir == "" {
		t.Skip("no cache dir")
	}
	if b, err := os.ReadFile(filepath.Join(dir, "sitecustomize.py")); err != nil || !strings.Contains(string(b), "AF_INET") {
		t.Fatalf("hook not written: %v", err)
	}
	sep := string(os.PathListSeparator)
	got := withIPv4First([]string{"A=1", "PYTHONPATH=/x"})
	if got[1] != "PYTHONPATH="+dir+sep+"/x" || got[0] != "A=1" {
		t.Errorf("prepend: %v", got)
	}
	got = withIPv4First([]string{"A=1"})
	if len(got) != 2 || got[1] != "PYTHONPATH="+dir {
		t.Errorf("add: %v", got)
	}
}

func TestRetryableCreate(t *testing.T) {
	cases := map[string]bool{
		"[colab] Allocation refused (precondition failed). This can mean too many active sessions": true,
		"HTTPSConnectionPool: Read timed out":                                                      true,
		"503 Service Unavailable":                                                                  true,
		"[colab] Backend rejected accelerator 'H100'. You may not have quota":                      false,
		"Colab CLI 的登录已失效，请重新登录":                                                                   false,
		"已有名为 demo 的会话，请换一个名字":                                                                     false,
		"已取消":                            false,
		"Traceback: KeyError 'endpoint'": false,
	}
	for msg, want := range cases {
		if got := retryableCreate(errors.New(msg)); got != want {
			t.Errorf("%q: retryable = %v, want %v", msg, got, want)
		}
	}
}

func TestPlainCreateError(t *testing.T) {
	e503 := plainCreateError(errors.New("ColabRequestError: Failed to issue request POST\nhttps://colab.research.google.com/tun/m/assign?nbh=x&variant=GPU&accelerator=G4: Service Unavailable"), "G4")
	if e503.Error() != "Colab 暂时没有空闲的 G4（503 Service Unavailable）" || !retryableCreate(e503) {
		t.Errorf("503: %v retryable=%v", e503, retryableCreate(e503))
	}
	e412 := plainCreateError(errors.New("[colab] Allocation refused (precondition failed)."), "A100")
	if !strings.Contains(e412.Error(), "412") || !retryableCreate(e412) {
		t.Errorf("412: %v", e412)
	}
	other := errors.New("[colab] Backend rejected accelerator 'H100'.")
	if plainCreateError(other, "H100") != other {
		t.Error("other errors stay as they are")
	}
	if waitText(30*time.Second) != " 30 秒" || waitText(2*time.Minute) != " 2 分钟" {
		t.Errorf("waitText: %q %q", waitText(30*time.Second), waitText(2*time.Minute))
	}
}

func TestRetryCreate(t *testing.T) {
	refused := errors.New("[colab] Allocation refused (precondition failed).")
	run := func(retries int, results ...error) (error, int, []string) {
		calls, notes := 0, []string{}
		err := retryCreate(context.Background(), retries, time.Millisecond, func() error {
			e := results[min(calls, len(results)-1)]
			calls++
			return e
		}, func(m string) { notes = append(notes, m) })
		return err, calls, notes
	}
	if err, calls, _ := run(3, refused, refused, nil); err != nil || calls != 3 {
		t.Errorf("succeeds on the third try: err=%v calls=%d", err, calls)
	}
	if err, calls, notes := run(2, refused); err == nil || calls != 3 || len(notes) != 2 || !strings.Contains(err.Error(), "试了 3 次") {
		t.Errorf("gives up after 2 retries: err=%v calls=%d notes=%v", err, calls, notes)
	}
	if err, calls, _ := run(5, errors.New("[colab] Backend rejected accelerator 'H100'.")); err == nil || calls != 1 {
		t.Errorf("no retry without entitlement: calls=%d", calls)
	}
	if _, calls, _ := run(0, refused); calls != 1 {
		t.Errorf("0 retries tries once: calls=%d", calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryCreate(ctx, 5, time.Hour, func() error { calls++; cancel(); return refused }, func(string) {})
	if err == nil || calls != 1 {
		t.Errorf("cancel stops the wait: err=%v calls=%d", err, calls)
	}
}
