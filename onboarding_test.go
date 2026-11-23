package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLoginFlowSignedOut runs the real colab CLI as a machine that never
// signed in: a temporary home (no legacy token file) and a keyring that
// stores nothing, so the user's own credentials are neither read nor
// changed. Opt in with COLAB_UI_LOGIN_TEST=1.
func TestLoginFlowSignedOut(t *testing.T) {
	if os.Getenv("COLAB_UI_LOGIN_TEST") == "" {
		t.Skip("set COLAB_UI_LOGIN_TEST=1 to run against the installed colab CLI")
	}
	bin, err := resolveColab("")
	if err != nil {
		t.Skip(err)
	}
	home := t.TempDir()
	t.Setenv("PYTHON_KEYRING_BACKEND", "keyring.backends.null.Keyring")
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOME", home)
	c := &Colab{cfg: &Settings{ColabPath: bin, Auth: "oauth2"}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	st := c.AuthStatus(ctx)
	if !st.NeedsLogin || st.LoggedIn {
		t.Fatalf("AuthStatus signed out = %+v", st)
	}

	url, err := c.LoginStart(ctx)
	if err != nil {
		t.Fatalf("LoginStart: %v", err)
	}
	if !strings.HasPrefix(url, "https://accounts.google.com/") {
		t.Fatalf("LoginStart url = %q", url)
	}

	st, err = c.LoginFinish(ctx, "not-a-real-code")
	if err == nil || st.LoggedIn {
		t.Fatalf("LoginFinish with a bad code = %+v, %v", st, err)
	}
	t.Logf("bad code -> %v", err)

	// Starting again and cancelling leaves no process behind.
	if _, err := c.LoginStart(ctx); err != nil {
		t.Fatalf("second LoginStart: %v", err)
	}
	c.LoginCancel()
	loginMu.Lock()
	defer loginMu.Unlock()
	if login != nil {
		t.Fatal("LoginCancel left a sign-in running")
	}
}
