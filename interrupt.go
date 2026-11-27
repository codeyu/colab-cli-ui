package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Stopping colab exec only disconnects it: the cell keeps running in the
// kernel, and the next one queues behind it. The CLI has no interrupt
// command, so the app interrupts the kernel through the runtime's Jupyter API
// itself, with the proxy URL and token the CLI keeps in its state file.

type sessionRecord struct {
	URL      string `json:"url"`
	Token    string `json:"token"`
	KernelID string `json:"kernel_id"`
	Endpoint string `json:"endpoint"`
	// Expires is when Token expires, as the CLI writes it (ISO 8601).
	Expires string `json:"token_expires_at"`
	// TokenExpiresAt is Expires parsed, nil when unknown.
	TokenExpiresAt *time.Time `json:"-"`
}

func (c *Colab) sessionRecord(name string) (sessionRecord, error) {
	p := c.settings().ConfigPath
	if p == "" {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, ".config", "colab-cli", "sessions.json")
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return sessionRecord{}, err
	}
	var all map[string]sessionRecord
	if err := json.Unmarshal(b, &all); err != nil {
		return sessionRecord{}, err
	}
	r, ok := all[name]
	if !ok || r.URL == "" || r.Token == "" {
		return sessionRecord{}, fmt.Errorf("没有会话 %s 的连接信息", name)
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.999999"} {
		if t, err := time.Parse(layout, r.Expires); err == nil {
			r.TokenExpiresAt = &t
			break
		}
	}
	return r, nil
}

func (r sessionRecord) request(ctx context.Context, method, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(r.URL, "/")+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Colab-Runtime-Proxy-Token", r.Token)
	req.Header.Set("X-Colab-Client-Agent", "colab-cli")
	return hfHTTP.Do(req)
}

// InterruptKernel interrupts the code running in the session's kernel, as a
// notebook's stop button does, keeping its variables.
func (c *Colab) InterruptKernel(ctx context.Context, name string) error {
	if err := checkName(name); err != nil {
		return err
	}
	rec, err := c.sessionRecord(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	ids := []string{}
	if rec.KernelID != "" {
		ids = append(ids, rec.KernelID)
	} else {
		// No kernel recorded yet: interrupt the busy ones.
		resp, err := rec.request(ctx, "GET", "/api/kernels")
		if err != nil {
			return err
		}
		var ks []struct {
			ID    string `json:"id"`
			State string `json:"execution_state"`
		}
		err = json.NewDecoder(resp.Body).Decode(&ks)
		resp.Body.Close()
		if err != nil {
			return err
		}
		for _, k := range ks {
			if k.State == "busy" {
				ids = append(ids, k.ID)
			}
		}
	}
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		resp, err := rec.request(ctx, "POST", "/api/kernels/"+id+"/interrupt")
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return errors.New("中断内核失败：" + resp.Status)
		}
	}
	return nil
}
