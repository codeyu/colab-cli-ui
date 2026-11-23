package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
)

// Settings are the app's preferences.
type Settings struct {
	// ColabPath is the colab executable; "" finds it on the PATH.
	ColabPath string `json:"colabPath"`
	// Auth is the CLI's authentication: "oauth2" (default) or "adc".
	Auth string `json:"auth"`
	// ConfigPath is the CLI's session state file; "" for its default.
	ConfigPath string `json:"configPath"`
	// OAuthConfig is a client OAuth config JSON; "" for the bundled one.
	OAuthConfig string `json:"oauthConfig"`
	// RefreshSeconds is how often the session list refreshes; 0 never.
	RefreshSeconds int `json:"refreshSeconds"`
	// HFEndpoint is where the app searches HuggingFace models, e.g. a
	// mirror; "" for https://huggingface.co.
	HFEndpoint string `json:"hfEndpoint"`
}

func defaultSettings() Settings { return Settings{Auth: "oauth2", RefreshSeconds: 30} }

func settingsFile() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

func (c *Colab) settings() Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cfg == nil {
		s := defaultSettings()
		if p, err := settingsFile(); err == nil {
			if b, err := os.ReadFile(p); err == nil {
				_ = json.Unmarshal(b, &s)
			}
		}
		c.cfg = &s
	}
	return *c.cfg
}

// GetSettings returns the app's preferences.
func (c *Colab) GetSettings() Settings { return c.settings() }

// SaveSettings stores the app's preferences.
func (c *Colab) SaveSettings(s Settings) error {
	if s.Auth != "" && s.Auth != "oauth2" && s.Auth != "adc" {
		return errors.New("认证方式只能是 oauth2 或 adc")
	}
	if s.RefreshSeconds < 0 {
		s.RefreshSeconds = 0
	}
	p, err := settingsFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return err
	}
	c.mu.Lock()
	c.cfg = &s
	c.mu.Unlock()
	return nil
}

// PickExecutable asks for the colab executable.
func (c *Colab) PickExecutable() (string, error) {
	files, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "选择 colab 可执行文件"})
	if err != nil || len(files) == 0 {
		return "", err
	}
	return files[0], nil
}
