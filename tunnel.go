package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/gorilla/websocket"
	"golang.org/x/crypto/ssh"
)

// Port forwarding: a local port on 127.0.0.1 reaches a port on the VM, so a
// Gradio app, ComfyUI or a model's API there opens in the local browser.
//
// The app speaks SSH to the runtime itself, over the runtime's /colab/ssh
// WebSocket, rather than running `colab ssh --proxy-mode`: given a session
// name it does not know, that command creates a new runtime, so a tunnel
// reconnecting after its session was released would quietly start a billed
// VM. Here a connection goes to the runtime's own URL, which simply fails
// once the runtime is gone.

// Forward is a local port forwarded to a port on a session's VM.
type Forward struct {
	ID         string `json:"id"`
	Session    string `json:"session"`
	LocalPort  int    `json:"localPort"`
	RemotePort int    `json:"remotePort"`
	Label      string `json:"label"`
	// State is "connecting", "open" or "error"; Error says why.
	State string `json:"state"`
	Error string `json:"error,omitempty"`
	// Conns is the number of connections open through it.
	Conns int `json:"conns"`
}

// ForwardsChanged is sent with the whole list when a forward changes.
var ForwardsChanged = mygo.NewEvent[[]Forward]("forwards-changed")

type forward struct {
	Forward
	ln net.Listener
}

// sshLink is the one SSH connection to a session's VM that its forwards share.
type sshLink struct {
	session  string
	endpoint string
	mu       sync.Mutex
	client   *ssh.Client
}

var tunnels = struct {
	sync.Mutex
	links    map[string]*sshLink
	forwards map[string]*forward
}{links: map[string]*sshLink{}, forwards: map[string]*forward{}}

// ListForwards lists the forwarded ports.
func (c *Colab) ListForwards() []Forward {
	tunnels.Lock()
	defer tunnels.Unlock()
	return forwardListLocked()
}

func forwardListLocked() []Forward {
	l := make([]Forward, 0, len(tunnels.forwards))
	for _, f := range tunnels.forwards {
		l = append(l, f.Forward)
	}
	sort.Slice(l, func(i, j int) bool { return l[i].ID < l[j].ID })
	return l
}

func broadcastForwards() {
	tunnels.Lock()
	l := forwardListLocked()
	tunnels.Unlock()
	ForwardsChanged.Broadcast(l)
}

func updateForward(id string, fn func(f *Forward)) {
	tunnels.Lock()
	f := tunnels.forwards[id]
	if f != nil {
		fn(&f.Forward)
	}
	tunnels.Unlock()
	if f != nil {
		broadcastForwards()
	}
}

// AddForward forwards 127.0.0.1:localPort to remotePort on the session's VM.
// localPort 0 takes remotePort when it is free, else any free port. It
// connects first, so a runtime that cannot be reached is reported here.
func (c *Colab) AddForward(ctx context.Context, session string, remotePort, localPort int, label string) (Forward, error) {
	if err := checkName(session); err != nil {
		return Forward{}, err
	}
	if remotePort < 1 || remotePort > 65535 || localPort < 0 || localPort > 65535 {
		return Forward{}, errors.New("端口应在 1–65535 之间")
	}
	tunnels.Lock()
	for _, f := range tunnels.forwards {
		if f.Session == session && f.RemotePort == remotePort {
			tunnels.Unlock()
			return f.Forward, nil
		}
	}
	tunnels.Unlock()

	link, err := c.linkFor(ctx, session)
	if err != nil {
		return Forward{}, err
	}
	if _, err := link.get(ctx, c); err != nil {
		return Forward{}, err
	}

	ln, err := listenLocal(localPort, remotePort)
	if err != nil {
		return Forward{}, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	f := &forward{Forward: Forward{
		ID:         fmt.Sprintf("%s:%d", session, port),
		Session:    session,
		LocalPort:  port,
		RemotePort: remotePort,
		Label:      strings.TrimSpace(label),
		State:      "open",
	}, ln: ln}
	tunnels.Lock()
	tunnels.forwards[f.ID] = f
	tunnels.Unlock()
	broadcastForwards()
	go c.serveForward(f, link)
	return f.Forward, nil
}

func listenLocal(localPort, remotePort int) (net.Listener, error) {
	if localPort > 0 {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if err != nil {
			return nil, fmt.Errorf("本机端口 %d 已被占用", localPort)
		}
		return ln, nil
	}
	if ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", remotePort)); err == nil {
		return ln, nil
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

// RemoveForward stops forwarding a port.
func (c *Colab) RemoveForward(id string) {
	tunnels.Lock()
	f := tunnels.forwards[id]
	delete(tunnels.forwards, id)
	tunnels.Unlock()
	if f != nil {
		f.ln.Close()
		closeIdleLinks()
		broadcastForwards()
	}
}

// closeForwardsOf stops the forwards of a session that was released.
func closeForwardsOf(session string) {
	tunnels.Lock()
	var gone []*forward
	for id, f := range tunnels.forwards {
		if f.Session == session {
			gone = append(gone, f)
			delete(tunnels.forwards, id)
		}
	}
	tunnels.Unlock()
	for _, f := range gone {
		f.ln.Close()
	}
	closeIdleLinks()
	if len(gone) > 0 {
		broadcastForwards()
	}
}

// dropForwardsExcept stops the forwards of sessions no longer running, as
// one the watchdog released.
func dropForwardsExcept(running []Session) {
	alive := map[string]bool{}
	for _, s := range running {
		if s.Local {
			alive[s.Name] = true
		}
	}
	tunnels.Lock()
	var gone []string
	for name := range tunnels.links {
		if !alive[name] {
			gone = append(gone, name)
		}
	}
	tunnels.Unlock()
	for _, n := range gone {
		closeForwardsOf(n)
	}
}

// closeIdleLinks closes the SSH connections no forward uses any more.
func closeIdleLinks() {
	tunnels.Lock()
	used := map[string]bool{}
	for _, f := range tunnels.forwards {
		used[f.Session] = true
	}
	var idle []*sshLink
	for name, l := range tunnels.links {
		if !used[name] {
			idle = append(idle, l)
			delete(tunnels.links, name)
		}
	}
	tunnels.Unlock()
	for _, l := range idle {
		l.close()
	}
}

func (c *Colab) serveForward(f *forward, link *sshLink) {
	for {
		conn, err := f.ln.Accept()
		if err != nil {
			return // closed
		}
		go c.pipe(f, link, conn)
	}
}

func (c *Colab) pipe(f *forward, link *sshLink, local net.Conn) {
	defer local.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	cl, err := link.get(ctx, c)
	cancel()
	if err != nil {
		updateForward(f.ID, func(x *Forward) { x.State, x.Error = "error", err.Error() })
		if errors.Is(err, errSessionGone) {
			closeForwardsOf(f.Session)
		}
		return
	}
	remote, err := cl.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", f.RemotePort))
	if err != nil {
		updateForward(f.ID, func(x *Forward) {
			x.State, x.Error = "error", fmt.Sprintf("虚拟机上的 %d 端口没有程序在监听", f.RemotePort)
		})
		return
	}
	defer remote.Close()
	updateForward(f.ID, func(x *Forward) { x.State, x.Error = "open", ""; x.Conns++ })
	defer updateForward(f.ID, func(x *Forward) { x.Conns-- })
	done := make(chan struct{}, 2)
	go func() { io.Copy(remote, local); remote.Close(); done <- struct{}{} }()
	go func() { io.Copy(local, remote); local.Close(); done <- struct{}{} }()
	<-done
	<-done
}

var errSessionGone = errors.New("会话已释放，端口转发已停止")

func (c *Colab) linkFor(ctx context.Context, session string) (*sshLink, error) {
	tunnels.Lock()
	l := tunnels.links[session]
	tunnels.Unlock()
	if l != nil {
		return l, nil
	}
	rec, err := c.sessionRecord(session)
	if err != nil {
		return nil, err
	}
	l = &sshLink{session: session, endpoint: rec.Endpoint}
	tunnels.Lock()
	if have := tunnels.links[session]; have != nil {
		l = have
	} else {
		tunnels.links[session] = l
	}
	tunnels.Unlock()
	return l, nil
}

func (l *sshLink) close() {
	l.mu.Lock()
	cl := l.client
	l.client = nil
	l.mu.Unlock()
	if cl != nil {
		cl.Close()
	}
}

// get returns the link's SSH client, connecting (again) when there is none.
// It connects only to the runtime the link was made for: a session of that
// name on another runtime, or none, is errSessionGone.
func (l *sshLink) get(ctx context.Context, c *Colab) (*ssh.Client, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.client != nil {
		return l.client, nil
	}
	rec, err := c.freshRecord(ctx, l.session)
	if err != nil || (l.endpoint != "" && rec.Endpoint != l.endpoint) {
		return nil, errSessionGone
	}
	cl, err := dialRuntimeSSH(ctx, rec)
	if err != nil {
		return nil, err
	}
	l.client = cl
	go func() {
		// Keepalives, which also find a dead connection before a request does.
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		wait := make(chan struct{})
		go func() { cl.Wait(); close(wait) }()
		for {
			select {
			case <-wait:
				l.mu.Lock()
				if l.client == cl {
					l.client = nil
				}
				l.mu.Unlock()
				return
			case <-t.C:
				if _, _, err := cl.SendRequest("keepalive@openssh.com", true, nil); err != nil {
					cl.Close()
				}
			}
		}
	}()
	return cl, nil
}

// freshRecord is the session's connection info with a token that is good for
// a while: when it is about to expire, a cheap CLI command (ls) renews it.
func (c *Colab) freshRecord(ctx context.Context, session string) (sessionRecord, error) {
	rec, err := c.sessionRecord(session)
	if err != nil {
		return rec, err
	}
	if rec.TokenExpiresAt == nil || time.Until(*rec.TokenExpiresAt) > 10*time.Minute {
		return rec, nil
	}
	if _, err := c.runQuiet(ctx, "", "ls", "-s", session, "content"); err != nil {
		return rec, err
	}
	return c.sessionRecord(session)
}

func dialRuntimeSSH(ctx context.Context, rec sessionRecord) (*ssh.Client, error) {
	signer, pub, err := appSSHKey()
	if err != nil {
		return nil, fmt.Errorf("SSH 密钥：%w", err)
	}
	u, err := url.Parse(rec.URL)
	if err != nil {
		return nil, err
	}
	scheme := "wss"
	if u.Scheme == "http" {
		scheme = "ws"
	}
	q := url.Values{}
	q.Set("colab-runtime-proxy-token", rec.Token)
	q.Add("colab-client-agent", "colab-cli")
	q.Add("colab-client-agent", "ssh-mode-proxy")
	wsURL := fmt.Sprintf("%s://%s/colab/ssh?%s", scheme, u.Host, q.Encode())
	h := http.Header{}
	h.Set("X-Colab-Ssh-Pubkey", pub)
	h.Set("X-Colab-Client-Agent", "colab-cli")
	d := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: 30 * time.Second}
	ws, resp, err := d.DialContext(ctx, wsURL, h)
	if err != nil {
		if resp != nil {
			return nil, handshakeError(resp)
		}
		return nil, fmt.Errorf("连不上虚拟机：%v", err)
	}
	conn := &wsConn{c: ws}
	cfg := &ssh.ClientConfig{
		User:            "root",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), // reached through the runtime's authenticated URL
		Timeout:         30 * time.Second,
	}
	if dl, ok := ctx.Deadline(); ok {
		ws.UnderlyingConn().SetDeadline(dl)
	}
	sc, chans, reqs, err := ssh.NewClientConn(conn, "colab-runtime:22", cfg)
	ws.UnderlyingConn().SetDeadline(time.Time{})
	if err != nil {
		ws.Close()
		return nil, fmt.Errorf("SSH 握手失败：%v", err)
	}
	return ssh.NewClient(sc, chans, reqs), nil
}

func handshakeError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	resp.Body.Close()
	switch resp.StatusCode {
	case 401:
		return errors.New("会话的访问令牌已失效，会话可能已被释放")
	case 404:
		return errors.New("这台虚拟机没有开启 SSH（较早创建的运行时），请新建一个会话")
	case 429:
		return errors.New("这台虚拟机的 SSH 连接数已满：关掉其它 colab ssh 或 IDE 连接后再试")
	case 502:
		return errors.New("虚拟机上的 sshd 无法连接，虚拟机可能不正常")
	}
	return fmt.Errorf("虚拟机拒绝了 SSH 连接（HTTP %d）：%s", resp.StatusCode, strings.TrimSpace(string(body)))
}

// appSSHKey is the app's own Ed25519 key, made on first use and kept in the
// user data directory, so forwarding needs no key of the user's. pub is the
// public key in authorized_keys form.
func appSSHKey() (ssh.Signer, string, error) {
	dir := sshKeyDir
	if dir == "" {
		var err error
		if dir, err = mygo.App.Path(mygo.PathUserData); err != nil {
			return nil, "", err
		}
	}
	path := filepath.Join(dir, "ssh", "id_ed25519")
	if b, err := os.ReadFile(path); err == nil {
		if s, err := ssh.ParsePrivateKey(b); err == nil {
			return s, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))), nil
		}
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	block, err := ssh.MarshalPrivateKey(priv, "colab-cli-ui")
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, "", err
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		return nil, "", err
	}
	return s, strings.TrimSpace(string(ssh.MarshalAuthorizedKey(s.PublicKey()))), nil
}

// sshKeyDir replaces the user data directory for the key in tests.
var sshKeyDir string

// wsConn is a WebSocket as a byte stream, for SSH to run over.
type wsConn struct {
	c  *websocket.Conn
	r  io.Reader
	wm sync.Mutex
}

func (w *wsConn) Read(p []byte) (int, error) {
	for {
		if w.r == nil {
			mt, r, err := w.c.NextReader()
			if err != nil {
				return 0, err
			}
			if mt != websocket.BinaryMessage && mt != websocket.TextMessage {
				continue
			}
			w.r = r
		}
		n, err := w.r.Read(p)
		if err == io.EOF {
			w.r = nil
			if n > 0 {
				return n, nil
			}
			continue
		}
		return n, err
	}
}

func (w *wsConn) Write(p []byte) (int, error) {
	w.wm.Lock()
	defer w.wm.Unlock()
	if err := w.c.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *wsConn) Close() error                       { return w.c.Close() }
func (w *wsConn) LocalAddr() net.Addr                { return w.c.LocalAddr() }
func (w *wsConn) RemoteAddr() net.Addr               { return w.c.RemoteAddr() }
func (w *wsConn) SetDeadline(t time.Time) error      { return w.c.UnderlyingConn().SetDeadline(t) }
func (w *wsConn) SetReadDeadline(t time.Time) error  { return w.c.SetReadDeadline(t) }
func (w *wsConn) SetWriteDeadline(t time.Time) error { return w.c.SetWriteDeadline(t) }
