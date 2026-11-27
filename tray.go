package main

import (
	_ "embed"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/egoist/mygo"
)

//go:embed resources/tray.png
var trayIcon []byte

//go:embed resources/tray-active.png
var trayActiveIcon []byte

// TraySession is a running session as the tray lists it.
type TraySession struct {
	Name     string `json:"name"`
	Hardware string `json:"hardware"`
	// Started is when the session was created, in Unix milliseconds; 0
	// when unknown.
	Started int64 `json:"started"`
	// Rate is the estimated compute units per hour.
	Rate float64 `json:"rate"`
}

// TrayStatus is what the page knows of the running sessions.
type TrayStatus struct {
	Sessions []TraySession `json:"sessions"`
	// RatePerHour is the account's rate as the server reports it, 0 when
	// unknown: then the sessions' estimates are summed.
	RatePerHour float64 `json:"ratePerHour"`
}

// OpenSession asks the page to show a session (from the tray menu).
var OpenSession = mygo.NewEvent[string]("open-session")

// desktop is the app's window and tray icon, and what they show.
var desktop struct {
	sync.Mutex
	win      *mygo.Window
	tray     *mygo.Tray
	status   TrayStatus
	active   bool
	quitting bool
	hinted   bool
}

// setupDesktop makes the tray icon and keeps the window in it, rather than
// quitting, while sessions are running: they cost compute units whether or
// not the window is open.
func setupDesktop(win *mygo.Window) {
	desktop.Lock()
	desktop.win = win
	desktop.Unlock()

	t, err := mygo.NewTray(mygo.TrayOptions{ID: "main", Icon: trayIcon, ToolTip: "Colab CLI UI"})
	if err != nil {
		log.Printf("tray: %v", err)
	} else {
		desktop.Lock()
		desktop.tray = t
		desktop.Unlock()
		t.OnClick(showMain)
		renderTray()
	}

	win.OnClose(func(e *mygo.CloseEvent) {
		desktop.Lock()
		n := len(desktop.status.Sessions)
		keep := !desktop.quitting && n > 0 && desktop.tray != nil
		hint := keep && !desktop.hinted
		if hint {
			desktop.hinted = true
		}
		desktop.Unlock()
		if !keep {
			return
		}
		e.PreventDefault()
		win.Hide()
		if hint {
			go notify("Colab CLI UI 仍在托盘里运行", fmt.Sprintf("还有 %d 台虚拟机在计费。右键托盘图标可以退出。", n))
		}
	})
	mygo.App.OnNotificationClick(func(string) { showMain() })

	// The elapsed times move on while nothing else changes.
	go func() {
		for range time.Tick(time.Minute) {
			renderTray()
		}
	}()
}

func showMain() {
	desktop.Lock()
	w := desktop.win
	desktop.Unlock()
	if w == nil {
		return
	}
	w.Show()
	if w.IsMinimized() {
		w.Restore()
	}
	w.Focus()
}

func quitApp() {
	desktop.Lock()
	desktop.quitting = true
	desktop.Unlock()
	mygo.App.Quit()
}

// SetTrayStatus tells the tray which sessions are running.
func (c *Colab) SetTrayStatus(st TrayStatus) {
	desktop.Lock()
	desktop.status = st
	desktop.Unlock()
	renderTray()
}

// Notify shows a desktop notification; clicking it brings the window back.
func (c *Colab) Notify(title, body string) error {
	return notify(title, body)
}

func notify(title, body string) error {
	if !mygo.NotificationsSupported() {
		return nil
	}
	return mygo.NewNotification(mygo.NotificationOptions{Title: title, Body: body}).Show()
}

func renderTray() {
	desktop.Lock()
	t, st := desktop.tray, desktop.status
	wasActive := desktop.active
	desktop.active = len(st.Sessions) > 0
	desktop.Unlock()
	if t == nil {
		return
	}
	now := time.Now()
	items := []*mygo.MenuItem{{Label: "打开 Colab CLI UI", Click: func(*mygo.MenuItem, *mygo.Window) { showMain() }}, mygo.Separator()}
	tip := "Colab CLI UI · 没有运行中的虚拟机"
	if len(st.Sessions) == 0 {
		items = append(items, &mygo.MenuItem{Label: "没有运行中的虚拟机", Disabled: true})
	} else {
		rate := st.RatePerHour
		if rate <= 0 {
			for _, s := range st.Sessions {
				rate += s.Rate
			}
		}
		head := fmt.Sprintf("%d 台运行中", len(st.Sessions))
		if rate > 0 {
			head += fmt.Sprintf(" · 约 %.2f CU/小时", rate)
		}
		tip = "Colab CLI UI · " + head
		items = append(items, &mygo.MenuItem{Label: head, Disabled: true})
		for _, s := range st.Sessions {
			name := s.Name
			items = append(items, &mygo.MenuItem{
				Label: s.Name,
				Badge: sessionBadge(s, now),
				Click: func(*mygo.MenuItem, *mygo.Window) {
					showMain()
					OpenSession.Broadcast(name)
				},
			})
		}
	}
	items = append(items, mygo.Separator(), &mygo.MenuItem{Label: "退出", Click: func(*mygo.MenuItem, *mygo.Window) { quitApp() }})
	t.SetMenu(mygo.NewMenu(items))
	t.SetToolTip(tip)
	if active := len(st.Sessions) > 0; active != wasActive {
		icon := trayIcon
		if active {
			icon = trayActiveIcon
		}
		if err := t.SetIcon(icon, false); err != nil {
			log.Printf("tray icon: %v", err)
		}
	}
}

// sessionBadge is "T4 · 1:23 · 约 1.5 CU".
func sessionBadge(s TraySession, now time.Time) string {
	b := s.Hardware
	if s.Started <= 0 {
		return b
	}
	d := now.Sub(time.UnixMilli(s.Started))
	b += " · " + shortDuration(d)
	if s.Rate > 0 {
		b += fmt.Sprintf(" · 约 %.1f CU", d.Hours()*s.Rate)
	}
	return b
}

// shortDuration is "1:05" for an hour and five minutes, "12 分" below one.
func shortDuration(d time.Duration) string {
	m := int(d.Minutes())
	if m < 60 {
		return fmt.Sprintf("%d 分", max(m, 0))
	}
	return fmt.Sprintf("%d:%02d", m/60, m%60)
}
