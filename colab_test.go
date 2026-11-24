package main

import (
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
