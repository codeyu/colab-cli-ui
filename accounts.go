package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

// Several Google accounts. The CLI keeps one sign-in, in the keyring entry
// "google-colab-cli"/"oauth_token", and one state file of sessions. The app
// keeps that pair as the "default" account, and gives every account added in
// the app a keyring entry and a state file of its own:
//
//   - a keyring backend on the CLI's PYTHONPATH (colabui_keyring) appends
//     "@<id>" to the service name, so the CLI reads and writes the
//     account's own sign-in, in the same system keyring;
//   - --config points at accounts/<id>/sessions.json in the user data
//     directory, so one account's runtimes never look gone to another.
//
// Nothing is swapped in or out of the CLI's own entry, so commands of
// several accounts can run at once, and `colab` in a terminal stays on the
// default account. A command for a session goes to the account that has the
// session; any other goes to the active account.

const defaultAccount = "default"

// SavedAccount is a Google account the app can use.
type SavedAccount struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	// Added is when it was added, in Unix milliseconds (0 for default).
	Added int64 `json:"added"`
}

type accountsFile struct {
	Active   string         `json:"active"`
	Accounts []SavedAccount `json:"accounts"`
}

var accountsMu sync.Mutex

func accountsPath() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "accounts.json"), nil
}

// loadAccounts reads the accounts, the default one always first.
func loadAccounts() accountsFile {
	f := accountsFile{}
	if p, err := accountsPath(); err == nil {
		if b, err := os.ReadFile(p); err == nil {
			_ = json.Unmarshal(b, &f)
		}
	}
	list := []SavedAccount{{ID: defaultAccount}}
	for _, a := range f.Accounts {
		if a.ID == defaultAccount {
			list[0].Email = a.Email
		} else if validAccountID(a.ID) {
			list = append(list, a)
		}
	}
	f.Accounts = list
	if f.Active == "" || f.find(f.Active) < 0 {
		f.Active = defaultAccount
	}
	return f
}

func (f accountsFile) find(id string) int {
	for i, a := range f.Accounts {
		if a.ID == id {
			return i
		}
	}
	return -1
}

func saveAccounts(f accountsFile) error {
	p, err := accountsPath()
	if err != nil {
		return err
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func validAccountID(id string) bool {
	if id == defaultAccount {
		return true
	}
	if len(id) != 12 {
		return false
	}
	_, err := hex.DecodeString(id)
	return err == nil && strings.ToLower(id) == id
}

func newAccountID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func activeAccount() string {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	return loadAccounts().Active
}

// accountKey pins the commands of a context to an account.
type accountKey struct{}

func withAccount(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, accountKey{}, id)
}

// statePath is the account's session state file, as the CLI uses it.
func (c *Colab) statePath(id string) string {
	if id != defaultAccount && id != "" {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err == nil {
			return filepath.Join(dir, "accounts", id, "sessions.json")
		}
	}
	if p := c.settings().ConfigPath; p != "" {
		return p
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "colab-cli", "sessions.json")
}

// configArgs is the --config flag for the account, if it needs one.
func (c *Colab) configArgs(id string) []string {
	if id == defaultAccount || id == "" {
		if p := c.settings().ConfigPath; p != "" {
			return []string{"--config", p}
		}
		return nil
	}
	p := c.statePath(id)
	os.MkdirAll(filepath.Dir(p), 0o700)
	return []string{"--config", p}
}

// accountEnv is what the CLI's environment needs for the account.
func accountEnv(id string) []string {
	if id == defaultAccount || id == "" {
		return nil
	}
	dir := keyringHookDir()
	if dir == "" {
		return []string{"COLAB_UI_KEYRING_NS=" + id, "PYTHON_KEYRING_BACKEND=colabui_keyring.Missing"}
	}
	return []string{"COLAB_UI_KEYRING_NS=" + id, "PYTHON_KEYRING_BACKEND=colabui_keyring.Namespaced", "COLAB_UI_PYPATH=" + dir}
}

// stateHas reports whether the account's state file records a session.
func (c *Colab) stateHas(id, session string) bool {
	b, err := os.ReadFile(c.statePath(id))
	if err != nil {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(b, &m) != nil {
		return false
	}
	_, ok := m[session]
	return ok
}

// accountFor is the account a command runs as: the one its context names,
// else for a command on a session (-s NAME) the account that has it, else
// the active one.
func (c *Colab) accountFor(ctx context.Context, args []string) string {
	if id, ok := ctx.Value(accountKey{}).(string); ok && id != "" {
		return id
	}
	accountsMu.Lock()
	f := loadAccounts()
	accountsMu.Unlock()
	if name := argAfter(args, "-s"); name != "" {
		if c.stateHas(f.Active, name) {
			return f.Active
		}
		for _, a := range f.Accounts {
			if a.ID != f.Active && c.stateHas(a.ID, name) {
				return a.ID
			}
		}
	}
	return f.Active
}

func argAfter(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

// sessionOwner is the account that has the session, "" when none does.
func (c *Colab) sessionOwner(name string) string {
	accountsMu.Lock()
	f := loadAccounts()
	accountsMu.Unlock()
	for _, a := range f.Accounts {
		if c.stateHas(a.ID, name) {
			return a.ID
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// Bound methods

// AccountsState lists the accounts and which one is active.
type AccountsState struct {
	Active   string         `json:"active"`
	Accounts []SavedAccount `json:"accounts"`
	// Supported is false with ADC sign-in, which has no accounts to switch.
	Supported bool `json:"supported"`
}

// Accounts lists the accounts.
func (c *Colab) Accounts() AccountsState {
	accountsMu.Lock()
	f := loadAccounts()
	accountsMu.Unlock()
	return AccountsState{Active: f.Active, Accounts: f.Accounts, Supported: c.settings().Auth != "adc"}
}

// SwitchAccount makes an account the active one.
func (c *Colab) SwitchAccount(id string) error {
	accountsMu.Lock()
	defer accountsMu.Unlock()
	f := loadAccounts()
	if f.find(id) < 0 {
		return errors.New("没有这个账号")
	}
	f.Active = id
	return saveAccounts(f)
}

// AccountUsage is an account's balance, for the account list.
type AccountUsage struct {
	ID          string  `json:"id"`
	SignedIn    bool    `json:"signedIn"`
	Balance     float64 `json:"balance"`
	RatePerHour float64 `json:"ratePerHour"`
	Assignments int     `json:"assignments"`
	Error       string  `json:"error,omitempty"`
}

// AccountsUsage reads every account's balance and running runtimes at once.
func (c *Colab) AccountsUsage(ctx context.Context) []AccountUsage {
	accounts := c.Accounts().Accounts
	out := make([]AccountUsage, len(accounts))
	var wg sync.WaitGroup
	for i, a := range accounts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			u := AccountUsage{ID: a.ID}
			res, err := c.usageOf(withAccount(ctx, a.ID), true)
			switch {
			case err == nil:
				u.SignedIn, u.Balance, u.RatePerHour, u.Assignments = true, res.Balance, res.RatePerHour, res.Assignments
			case needsLogin(err.Error()):
				u.Error = "需要重新登录"
			default:
				u.Error = tail(err.Error(), 200)
			}
			out[i] = u
		}()
	}
	wg.Wait()
	return out
}

// AddAccountStart starts signing in another Google account and returns the
// Google address to open; LoginFinish completes it.
func (c *Colab) AddAccountStart(ctx context.Context) (string, error) {
	id := newAccountID()
	u, err := c.loginStart(ctx, id)
	if err == nil && u == "" {
		return "", errors.New("新账号的登录没有开始，请重试")
	}
	return u, err
}

// finishAdd records an account that has just signed in, and makes it active.
// The same Google account signed in twice keeps its first entry.
func (c *Colab) finishAdd(id, email string) (string, error) {
	accountsMu.Lock()
	f := loadAccounts()
	// Signing in again (a lapsed sign-in) keeps the entry.
	if i := f.find(id); i >= 0 {
		if email != "" {
			f.Accounts[i].Email = email
		}
		err := saveAccounts(f)
		accountsMu.Unlock()
		return id, err
	}
	for _, a := range f.Accounts {
		if email != "" && strings.EqualFold(a.Email, email) {
			f.Active = a.ID
			err := saveAccounts(f)
			accountsMu.Unlock()
			go c.forgetSignIn(id)
			return a.ID, err
		}
	}
	f.Accounts = append(f.Accounts, SavedAccount{ID: id, Email: email, Added: time.Now().UnixMilli()})
	f.Active = id
	err := saveAccounts(f)
	accountsMu.Unlock()
	return id, err
}

// rememberEmail records an account's address as whoami reports it.
func (c *Colab) rememberEmail(id, email string) {
	if email == "" {
		return
	}
	accountsMu.Lock()
	defer accountsMu.Unlock()
	f := loadAccounts()
	if i := f.find(id); i >= 0 && f.Accounts[i].Email != email {
		f.Accounts[i].Email = email
		_ = saveAccounts(f)
	}
}

// RemoveAccount forgets an account added in the app, and its sign-in. One
// that still has runtimes this app started is refused: they would bill on
// with nothing left to stop them.
func (c *Colab) RemoveAccount(ctx context.Context, id string) error {
	if id == defaultAccount {
		return errors.New("默认账号是 Colab CLI 自己的登录，不能移除")
	}
	accountsMu.Lock()
	f := loadAccounts()
	accountsMu.Unlock()
	i := f.find(id)
	if i < 0 {
		return errors.New("没有这个账号")
	}
	if b, err := os.ReadFile(c.statePath(id)); err == nil {
		var m map[string]json.RawMessage
		if json.Unmarshal(b, &m) == nil && len(m) > 0 {
			return fmt.Errorf("账号 %s 还有 %d 个会话，请先释放", f.Accounts[i].Email, len(m))
		}
	}
	c.forgetSignIn(id)
	accountsMu.Lock()
	f = loadAccounts()
	if j := f.find(id); j >= 0 {
		f.Accounts = append(f.Accounts[:j], f.Accounts[j+1:]...)
	}
	if f.Active == id {
		f.Active = defaultAccount
	}
	err := saveAccounts(f)
	accountsMu.Unlock()
	os.RemoveAll(filepath.Dir(c.statePath(id)))
	return err
}

// forgetSignIn deletes an account's token from the keyring, with the CLI's
// own Python, which has the keyring module.
func (c *Colab) forgetSignIn(id string) {
	if id == defaultAccount {
		return
	}
	py := c.cliPython()
	if py == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, py, "-c", "import colabui_keyring; colabui_keyring.forget()")
	cmd.Env = c.cliEnv(accountEnv(id)...)
	hideWindow(cmd)
	_ = cmd.Run()
}

// cliPython is the Python of the CLI's uv tool environment, "" if unknown.
func (c *Colab) cliPython() string {
	name, sub := "python", "bin"
	if runtime.GOOS == "windows" {
		name, sub = "python.exe", "Scripts"
	}
	if bin, err := resolveColab(c.settings().ColabPath); err == nil {
		if real, err := filepath.EvalSymlinks(bin); err == nil {
			bin = real
		}
		if p := filepath.Join(filepath.Dir(bin), name); fileExists(p) {
			return p
		}
	}
	if uv, err := resolveUV(); err == nil && uv != "" {
		cmd := exec.Command(uv, "tool", "dir")
		hideWindow(cmd)
		if out, err := cmd.Output(); err == nil {
			p := filepath.Join(strings.TrimSpace(string(out)), "google-colab-cli", sub, name)
			if fileExists(p) {
				return p
			}
		}
	}
	return ""
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// ---------------------------------------------------------------------------
// The keyring backend

const keyringPy = `# Written by Colab CLI UI: keeps each account's Colab CLI sign-in in a
# keyring entry of its own, the service name with "@<account>" appended.
# Chosen with PYTHON_KEYRING_BACKEND=colabui_keyring.Namespaced and
# COLAB_UI_KEYRING_NS=<account>.
import os
from keyring.backend import KeyringBackend


def _ns():
    ns = os.environ.get("COLAB_UI_KEYRING_NS", "")
    if not ns:
        raise RuntimeError("COLAB_UI_KEYRING_NS is not set")
    return ns


class Namespaced(KeyringBackend):
    priority = 1
    _real = None

    @classmethod
    def real(cls):
        if cls._real is None:
            from keyring.backend import get_all_keyring
            from keyring.backends import chainer, fail
            found = [k for k in get_all_keyring() if not isinstance(k, (Namespaced, chainer.ChainerBackend, fail.Keyring))]
            if not found:
                raise RuntimeError("no system keyring")
            cls._real = max(found, key=lambda k: k.priority)
        return cls._real

    def get_password(self, service, username):
        return self.real().get_password(service + "@" + _ns(), username)

    def set_password(self, service, username, password):
        self.real().set_password(service + "@" + _ns(), username, password)

    def delete_password(self, service, username):
        self.real().delete_password(service + "@" + _ns(), username)


def forget():
    try:
        Namespaced().delete_password("google-colab-cli", "oauth_token")
    except Exception as e:
        print(type(e).__name__)
    print("__COLAB_UI_FORGOT__")
`

var (
	keyringOnce sync.Once
	keyringDir  string
)

// keyringHookDir writes the backend module once and returns its directory,
// or "" when it cannot.
func keyringHookDir() string {
	keyringOnce.Do(func() {
		base, err := os.UserCacheDir()
		if err != nil {
			return
		}
		dir := filepath.Join(base, "colab-cli-ui", "pykeyring")
		if os.MkdirAll(dir, 0o755) != nil {
			return
		}
		if os.WriteFile(filepath.Join(dir, "colabui_keyring.py"), []byte(keyringPy), 0o644) != nil {
			return
		}
		keyringDir = dir
	})
	return keyringDir
}
