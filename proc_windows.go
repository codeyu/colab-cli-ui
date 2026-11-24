//go:build windows

package main

import (
	"os/exec"
	"strings"
	"syscall"
)

const createNoWindow = 0x08000000

// hideWindow keeps the console of colab (a console program) from flashing up.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// openTerminal runs colab with args in a new console window, for the commands
// that need a TTY (repl, ssh, auth, drivemount).
func openTerminal(bin string, args []string, title string, env []string) error {
	parts := []string{`"` + bin + `"`}
	for _, a := range args {
		if strings.ContainsAny(a, " \t") {
			a = `"` + a + `"`
		}
		parts = append(parts, a)
	}
	inner := strings.Join(parts, " ")
	if wt, err := exec.LookPath("wt.exe"); err == nil {
		cmd := exec.Command(wt)
		cmd.Env = env
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CmdLine: `wt.exe new-tab --title "` + title + `" cmd.exe /k "` + inner + `"`,
		}
		if err := cmd.Start(); err == nil {
			go cmd.Wait()
			return nil
		}
	}
	cmd := exec.Command("cmd.exe")
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:    `cmd.exe /c start "` + title + `" cmd.exe /k "` + inner + `"`,
		HideWindow: true,
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	go cmd.Wait()
	return nil
}
