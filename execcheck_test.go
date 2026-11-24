package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The order seen on a real VM: a shell cell failed just before an install,
// and another run on the same VM raised while the install ran.
func TestCellErrorMatchesTheCell(t *testing.T) {
	historyRoot = t.TempDir()
	defer func() { historyRoot = "" }()
	start := time.Now()
	at := func(d time.Duration) string { return start.Add(d).UTC().Format("2006-01-02T15:04:05.000000+00:00") }
	errOut := func(name string) []map[string]any {
		return []map[string]any{{"output_type": "error", "ename": name, "evalue": "boom"}}
	}
	events := []map[string]any{
		{"timestamp": at(-500 * time.Millisecond), "event_type": "execution", "code": "%%bash\nls /nope\n", "outputs": errOut("CalledProcessError")},
		{"timestamp": at(time.Second), "event_type": "execution", "code": "print('other')\n1/0\n", "outputs": errOut("ZeroDivisionError")},
		{"timestamp": at(2 * time.Second), "event_type": "automation_result", "op": "install", "outputs": errOut("PipError")},
		{"timestamp": at(3 * time.Second), "event_type": "execution", "code": "x = 1\r\nprint(x)\r\n", "outputs": []any{}},
	}
	var b strings.Builder
	for _, e := range events {
		j, _ := json.Marshal(e)
		b.Write(j)
		b.WriteByte('\n')
	}
	os.WriteFile(filepath.Join(historyRoot, "s.jsonl"), []byte(b.String()), 0o644)

	var ee *ExecError
	if err := cellError("s", start, installResult); !errors.As(err, &ee) || ee.Name != "PipError" {
		t.Errorf("install: %v", err)
	}
	if err := cellError("s", start, cellsOf("print('other')\n1/0\n")); !errors.As(err, &ee) || ee.Name != "ZeroDivisionError" {
		t.Errorf("own cell: %v", err)
	}
	// A clean cell is clean although others failed around it; CRLF in a
	// local file matches the LF the CLI records, and the reverse.
	if err := cellError("s", start, cellsOf("x = 1\nprint(x)\n")); err != nil {
		t.Errorf("clean cell: %v", err)
	}
	// The shell cell failed before the run started.
	if err := cellError("s", start, cellsOf("%%bash\nls /nope\n")); err != nil {
		t.Errorf("earlier cell: %v", err)
	}
}

func TestFileCellsOfNotebook(t *testing.T) {
	p := filepath.Join(t.TempDir(), "n.ipynb")
	os.WriteFile(p, []byte(`{"cells":[{"cell_type":"markdown","source":"# hi"},{"cell_type":"code","source":["a = 1\n","print(a)"]},{"cell_type":"code","source":"b = 2"}]}`), 0o644)
	got := fileCells(p)
	if len(got) != 2 || got[0] != "a = 1\nprint(a)" || got[1] != "b = 2" {
		t.Errorf("cells = %q", got)
	}
}
