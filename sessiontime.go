package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// startTimes caches when sessions were created, by name and endpoint.
var startTimes sync.Map

// sessionStarted is when the CLI created the session with this name and
// endpoint, in Unix milliseconds, from its history log ("session_created"),
// or 0 when the log does not say (a runtime created elsewhere).
func sessionStarted(name, endpoint string) int64 {
	if name == "" || endpoint == "" || filepath.Base(name) != name {
		return 0
	}
	key := name + "\x00" + endpoint
	if v, ok := startTimes.Load(key); ok {
		return v.(int64)
	}
	f, err := os.Open(filepath.Join(historyDir(), name+".jsonl"))
	if err != nil {
		return 0
	}
	defer f.Close()
	var started int64
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	ep := []byte(`"` + endpoint + `"`)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"session_created"`)) || !bytes.Contains(line, ep) {
			continue
		}
		var ev struct {
			Timestamp time.Time `json:"timestamp"`
			Endpoint  string    `json:"endpoint"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.Endpoint == endpoint && !ev.Timestamp.IsZero() {
			started = ev.Timestamp.UnixMilli()
		}
	}
	if started > 0 {
		startTimes.Store(key, started)
	}
	return started
}
