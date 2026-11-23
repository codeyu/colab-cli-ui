package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"
)

// StartupScript is a list of code steps run on a new session right after it
// is created, e.g. installing packages and loading a model.
type StartupScript struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Steps []Step `json:"steps"`
	// Drive mounts Google Drive before the steps run.
	Drive   bool      `json:"drive"`
	Updated time.Time `json:"updated"`
}

// Step is one piece of Python code of a startup script.
type Step struct {
	Code string `json:"code"`
}

func scriptsFile() (string, error) {
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "startup-scripts.json"), nil
}

func (c *Colab) loadScripts() ([]StartupScript, error) {
	p, err := scriptsFile()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return []StartupScript{}, nil
	}
	if err != nil {
		return nil, err
	}
	var list []StartupScript
	if err := json.Unmarshal(b, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (c *Colab) storeScripts(list []StartupScript) error {
	p, err := scriptsFile()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(list, "", "  ")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// ListScripts returns the startup scripts, most recently changed first.
func (c *Colab) ListScripts() ([]StartupScript, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	list, err := c.loadScripts()
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Updated.After(list[j].Updated) })
	return list, nil
}

// SaveScript creates a startup script, or replaces the one with its ID.
func (c *Colab) SaveScript(s StartupScript) (StartupScript, error) {
	s.Name = strings.TrimSpace(s.Name)
	if s.Name == "" {
		return s, errors.New("请给脚本起个名字")
	}
	steps := s.Steps[:0]
	for _, st := range s.Steps {
		if strings.TrimSpace(st.Code) != "" {
			steps = append(steps, st)
		}
	}
	if len(steps) == 0 {
		return s, errors.New("脚本里没有代码")
	}
	s.Steps = steps
	s.Updated = time.Now()

	c.mu.Lock()
	defer c.mu.Unlock()
	list, err := c.loadScripts()
	if err != nil {
		return s, err
	}
	if s.ID == "" {
		b := make([]byte, 6)
		rand.Read(b)
		s.ID = hex.EncodeToString(b)
		list = append(list, s)
	} else {
		found := false
		for i := range list {
			if list[i].ID == s.ID {
				list[i], found = s, true
			}
		}
		if !found {
			list = append(list, s)
		}
	}
	return s, c.storeScripts(list)
}

// DeleteScript removes a startup script.
func (c *Colab) DeleteScript(id string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	list, err := c.loadScripts()
	if err != nil {
		return err
	}
	kept := list[:0]
	for _, s := range list {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	return c.storeScripts(kept)
}
