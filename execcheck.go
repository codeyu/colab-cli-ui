package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// colab exec exits 0 even when the code raises: the traceback only goes to
// stderr. The CLI does record every cell it ran, with its outputs, in the
// session's history file before it exits, so that record tells whether a run
// failed: a cell with an output of type "error".

// ExecError is code that raised on the VM.
type ExecError struct {
	Name  string // the exception's class, e.g. ModuleNotFoundError
	Value string
}

func (e *ExecError) Error() string {
	if e.Value == "" {
		return "代码出错：" + e.Name
	}
	return "代码出错：" + e.Name + ": " + e.Value
}

// cellError returns the first error raised by a cell of the session that
// finished at or after since and that match accepts, or nil. Matching the
// cell, not just the time, keeps another run on the same VM (the resource
// monitor, a second window) from being blamed.
func cellError(name string, since time.Time, match func(cell) bool) error {
	f, err := os.Open(filepath.Join(historyDir(), name+".jsonl"))
	if err != nil {
		return nil // no record: nothing to tell
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"error"`) {
			continue
		}
		var ev cell
		if json.Unmarshal(line, &ev) != nil {
			continue
		}
		at, err := time.Parse(time.RFC3339Nano, ev.Timestamp)
		if err != nil || at.Before(since) || !match(ev) {
			continue
		}
		for _, o := range ev.Outputs {
			if o.OutputType == "error" {
				return &ExecError{Name: o.Ename, Value: o.Evalue}
			}
		}
	}
	return nil
}

// cell is an entry of the CLI's history: a cell it ran (execution) or the
// result of an automation such as install (automation_result).
type cell struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"event_type"`
	Op        string `json:"op"`
	Code      string `json:"code"`
	Outputs   []struct {
		OutputType string `json:"output_type"`
		Ename      string `json:"ename"`
		Evalue     string `json:"evalue"`
	} `json:"outputs"`
}

func normNL(s string) string {
	return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
}

// cellsOf matches the execution cells whose code is one of codes.
func cellsOf(codes ...string) func(cell) bool {
	want := map[string]bool{}
	for _, c := range codes {
		want[normNL(c)] = true
	}
	return func(c cell) bool { return c.Type == "execution" && want[normNL(c.Code)] }
}

// fileCells returns the code a file runs as: the file, or a notebook's code
// cells.
func fileCells(file string) []string {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	if !strings.HasSuffix(strings.ToLower(file), ".ipynb") {
		return []string{string(b)}
	}
	var nb struct {
		Cells []struct {
			Type   string          `json:"cell_type"`
			Source json.RawMessage `json:"source"`
		} `json:"cells"`
	}
	if json.Unmarshal(b, &nb) != nil {
		return nil
	}
	var codes []string
	for _, c := range nb.Cells {
		if c.Type != "code" {
			continue
		}
		var s string
		var parts []string
		if json.Unmarshal(c.Source, &s) != nil && json.Unmarshal(c.Source, &parts) == nil {
			s = strings.Join(parts, "")
		}
		codes = append(codes, s)
	}
	return codes
}

// installResult matches the result of `colab install`.
func installResult(c cell) bool { return c.Type == "automation_result" && c.Op == "install" }

// internalMarker starts the code the app runs for itself, so that history
// views and startup scripts can leave it out.
const internalMarker = "# __COLAB_UI_INTERNAL__"

func internal(code string) string { return internalMarker + "\n" + code }

// execIdleTimeout is how long colab exec waits without any output. Not
// unlimited (0): the CLI's wait for the kernel connection is bounded by it
// too, and a connection that never comes up would hang forever.
const execIdleTimeout = "900"

// execFailure explains the CLI giving up on a silent cell.
func execFailure(err error) error {
	if strings.Contains(err.Error(), "Timeout waiting for") {
		return fmt.Errorf("15 分钟没有任何输出，已停止等待。代码可能仍在 VM 上运行，可以在「资源监控」里查看，或重启内核")
	}
	return err
}
