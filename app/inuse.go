package main

import (
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/fanfree/fanfree/engine/scan"
)

// In-use detection keeps a clean from pulling caches out from under a running program. Two
// signals: something under the item was written in the last few minutes, or the item belongs to
// an app that is running now.

// appCaches maps a process name (lower case substring) to path fragments its caches live under.
var appCaches = []struct {
	app   string
	label string
	paths []string
}{
	{"google chrome", "Chrome", []string{"/google/chrome/", `\google\chrome\`}},
	{"chrome.exe", "Chrome", []string{`\google\chrome\`}},
	{"chrome", "Chrome", []string{"/google-chrome/", "/chromium/"}},
	{"microsoft edge", "Edge", []string{"/microsoft edge/", `\microsoft\edge\`}},
	{"msedge", "Edge", []string{`\microsoft\edge\`, "/microsoft-edge/"}},
	{"brave", "Brave", []string{"/bravesoftware/", `\bravesoftware\`}},
	{"firefox", "Firefox", []string{"/firefox/", "/mozilla/", `\mozilla\`}},
	{"slack", "Slack", []string{"/slack/", `\slack\`}},
	{"discord", "Discord", []string{"/discord/", `\discord\`}},
	{"code helper", "VS Code", []string{"/code/", `\code\`}},
	{"electron", "VS Code", []string{"/code/"}},
	{"code.exe", "VS Code", []string{`\code\`}},
	{"xcode", "Xcode", []string{"/deriveddata/", "/xcode/"}},
	{"simulator", "Simulator", []string{"/coresimulator/"}},
}

// runningApps lists process names, lower case. It runs a subprocess: call it off the main thread.
func runningApps() []string {
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("tasklist", "/fo", "csv", "/nh")
	} else {
		cmd = exec.Command("ps", "-axo", "comm=")
	}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.ToLower(strings.TrimSpace(l))
		if runtime.GOOS == "windows" {
			l = strings.Trim(strings.SplitN(l, ",", 2)[0], `"`)
		} else if i := strings.LastIndexByte(l, '/'); i >= 0 {
			l = l[i+1:]
		}
		if l != "" {
			names = append(names, l)
		}
	}
	return names
}

// appsUsing names the running app that has path in use, or "".
func appsUsing(path string, procs []string) string {
	lp := strings.ToLower(path)
	for _, ac := range appCaches {
		hit := false
		for _, f := range ac.paths {
			if strings.Contains(lp, f) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		for _, p := range procs {
			if strings.Contains(p, ac.app) {
				return ac.label
			}
		}
	}
	return ""
}

// newestWrite is the latest modification time anywhere in i's subtree.
func newestWrite(t *scan.Tree, i int32) uint32 {
	m := t.Nodes[i].MTime
	stack := []int32{i}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for c := t.Nodes[n].First; c != scan.None; c = t.Nodes[c].Next {
			if t.Nodes[c].MTime > m {
				m = t.Nodes[c].MTime
			}
			if t.Nodes[c].First != scan.None {
				stack = append(stack, c)
			}
		}
	}
	return m
}

// recentWrite is the in-use reason of an item written to a moment ago; any other reason is
// the name of the app that holds it.
const recentWrite = "\x00write"

// recentWrites marks items written within window. It walks the tree and runs on the main thread.
func recentWrites(t *scan.Tree, items []cleanItem, window time.Duration, now time.Time) map[int32]string {
	out := map[int32]string{}
	if window <= 0 {
		return out
	}
	cut := uint32(now.Add(-window).Unix())
	for _, it := range items {
		if m := newestWrite(t, it.node); m >= cut {
			out[it.node] = recentWrite
		}
	}
	return out
}
