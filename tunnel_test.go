package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestTunnelLive forwards the VM's Jupyter port (8080) of the session named
// by COLAB_UI_TUNNEL_TEST and fetches /api through it.
func TestTunnelLive(t *testing.T) {
	name := os.Getenv("COLAB_UI_TUNNEL_TEST")
	if name == "" {
		t.Skip("set COLAB_UI_TUNNEL_TEST to a running session")
	}
	sshKeyDir = t.TempDir()
	c := &Colab{}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	start := time.Now()
	f, err := c.AddForward(ctx, name, 8080, 0, "jupyter")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("forward %+v in %v", f, time.Since(start))
	for i := 0; i < 3; i++ {
		resp, err := http.Get("http://127.0.0.1:" + itoa(f.LocalPort) + "/api")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Logf("GET /api %d %s", resp.StatusCode, b)
		if !strings.Contains(string(b), "version") {
			t.Fatalf("unexpected body %q", b)
		}
	}
	// Drop the SSH connection: the next request reconnects.
	tunnels.Lock()
	l := tunnels.links[name]
	tunnels.Unlock()
	l.close()
	resp, err := http.Get("http://127.0.0.1:" + itoa(f.LocalPort) + "/api")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	t.Logf("after reconnect %d", resp.StatusCode)
	c.RemoveForward(f.ID)
	if len(c.ListForwards()) != 0 {
		t.Fatal("forward not removed")
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
