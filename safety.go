package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// Things that cost money when they go wrong: runtimes left behind by a
// create that failed or was cancelled, runtimes without a local record, and
// stops that silently did nothing.

// Notice is a message for the user from work that ends after the call that
// started it, such as cleaning up after a cancelled create.
type Notice struct {
	Kind   string `json:"kind"` // success, error or info
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// Notices are shown as toasts.
var Notices = mygo.NewEvent[Notice]("notice")

// AuthLost is sent when a command found the CLI signed out: the page goes
// back to the sign-in step.
var AuthLost = mygo.NewEvent[bool]("auth-lost")

var endpointRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{3,80}$`)

// ReleaseOrphan stops a runtime the CLI has no local record of (shown as [?],
// e.g. one opened in the browser): `colab stop` can only address a runtime by
// a recorded name, so it runs against a throwaway state file that records the
// endpoint under a placeholder name, and the CLI's own client releases it.
func (c *Colab) ReleaseOrphan(ctx context.Context, endpoint string) error {
	if !endpointRe.MatchString(endpoint) {
		return fmt.Errorf("endpoint 无效：%q", endpoint)
	}
	dir, err := os.MkdirTemp("", "colab-ui-orphan-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	state := filepath.Join(dir, "sessions.json")
	rec := map[string]any{"orphan": map[string]any{
		"name": "orphan", "token": "", "url": "https://localhost.invalid", "endpoint": endpoint,
	}}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(state, b, 0o600); err != nil {
		return err
	}
	if _, err := c.run(ctx, "", "--config", state, "stop", "-s", "orphan"); err != nil {
		return err
	}
	// `colab stop` reports success whether or not anything was released.
	gone, err := c.endpointGone(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("已请求释放，但无法确认：%w", err)
	}
	if !gone {
		return errors.New("释放请求已发出，但服务器上仍有这个运行时")
	}
	return nil
}

func (c *Colab) serverEndpoints(ctx context.Context) (map[string]Session, error) {
	out, err := c.run(ctx, "", "sessions")
	if err != nil {
		return nil, err
	}
	m := map[string]Session{}
	for _, s := range parseSessions(out) {
		m[s.Endpoint] = s
	}
	return m, nil
}

func (c *Colab) endpointGone(ctx context.Context, endpoint string) (bool, error) {
	eps, err := c.serverEndpoints(ctx)
	if err != nil {
		return false, err
	}
	_, still := eps[endpoint]
	return !still, nil
}

// reapAfterFailedNew stops whatever a failed or cancelled `colab new` may
// have allocated anyway: the request can still be granted after the CLI gave
// up or was killed. It looks right away and again a little later, and
// returns what it did, for the error message.
func (c *Colab) reapAfterFailedNew(name string, before map[string]Session) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var done []string
	for _, wait := range []time.Duration{3 * time.Second, 20 * time.Second} {
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return strings.Join(done, "；")
		}
		eps, err := c.serverEndpoints(ctx)
		if err != nil {
			continue
		}
		for ep, s := range eps {
			if _, old := before[ep]; old {
				continue
			}
			var err error
			if s.Name == name {
				_, err = c.run(ctx, "", "stop", "-s", name)
			} else if s.Name == "?" {
				err = c.ReleaseOrphan(ctx, ep)
			} else {
				continue // someone else's new runtime
			}
			if err != nil {
				msg := fmt.Sprintf("新分配的运行时 %s 释放失败：%v", ep, err)
				done = append(done, msg)
				Notices.Broadcast(Notice{Kind: "error", Title: "可能有运行时仍在计费", Detail: msg})
			} else {
				done = append(done, "已释放后台分配的运行时 "+ep)
				Notices.Broadcast(Notice{Kind: "info", Title: "已释放取消时分配的运行时", Detail: ep})
			}
		}
	}
	return strings.Join(done, "；")
}
