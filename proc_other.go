//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

func hideWindow(*exec.Cmd) {}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// openTerminal runs colab with args in a new terminal window, for the commands
// that need a TTY (repl, ssh, auth, drivemount).
func openTerminal(bin string, args []string, title string) error {
	line := shellQuote(bin)
	for _, a := range args {
		line += " " + shellQuote(a)
	}
	switch runtime.GOOS {
	case "darwin":
		script := `tell application "Terminal" to do script "` + strings.ReplaceAll(line, `"`, `\"`) + `"
tell application "Terminal" to activate`
		return exec.Command("osascript", "-e", script).Start()
	default:
		for _, term := range [][]string{
			{"x-terminal-emulator", "-e", "sh", "-c", line + "; exec $SHELL"},
			{"gnome-terminal", "--", "sh", "-c", line + "; exec $SHELL"},
			{"konsole", "-e", "sh", "-c", line + "; exec $SHELL"},
			{"xterm", "-T", title, "-e", "sh", "-c", line + "; exec $SHELL"},
		} {
			if _, err := exec.LookPath(term[0]); err == nil {
				return exec.Command(term[0], term[1:]...).Start()
			}
		}
		return errors.New("没有找到可用的终端程序")
	}
}
