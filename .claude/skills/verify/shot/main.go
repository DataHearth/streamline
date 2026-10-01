// Command shot logs into a running streamline instance, screenshots each given
// SPA path, and prints the console errors, uncaught exceptions and failed
// requests each page produced. It is the browser fallback for sessions with no
// browser tool (cloud sessions, other contributors' machines).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
)

func main() {
	base := flag.String("url", "http://127.0.0.1:18080", "instance base URL")
	email := flag.String("email", "verify@streamline.local", "login email")
	password := flag.String("password", "Verify-Passw0rd!", "login password")
	out := flag.String("out", ".", "directory for the screenshots")
	width := flag.Int("width", 1440, "viewport width (390 for a phone)")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: shot [flags] /path [/path...]")
		os.Exit(2)
	}

	// rod's auto-downloaded chromium cannot run on NixOS; the devshell
	// exports a working one.
	l := launcher.New().Headless(true)
	if bin := os.Getenv("CHROME_PATH"); bin != "" {
		l = l.Bin(bin)
	}
	defer l.Kill()
	browser := rod.New().ControlURL(l.MustLaunch()).MustConnect()
	defer browser.MustClose()

	page := browser.MustPage(*base + "/login").MustWaitLoad()
	page.MustSetViewport(*width, 900, 1, false)
	status := page.MustEval(`(e, p) => fetch("/auth/login", {
		method: "POST",
		headers: {"Content-Type": "application/json"},
		body: JSON.stringify({email: e, password: p}),
	}).then(r => r.status)`, *email, *password).Int()
	if status != 204 {
		fmt.Fprintf(os.Stderr, "login answered %d\n", status)
		os.Exit(1)
	}

	var (
		mu     sync.Mutex
		issues []string
	)
	note := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		issues = append(issues, s)
	}
	go page.EachEvent(
		func(e *proto.RuntimeConsoleAPICalled) {
			if e.Type == proto.RuntimeConsoleAPICalledTypeError ||
				e.Type == proto.RuntimeConsoleAPICalledTypeWarning {
				parts := make([]string, 0, len(e.Args))
				for _, a := range e.Args {
					parts = append(parts, page.MustObjectToJSON(a).String())
				}
				note(fmt.Sprintf("console.%s: %s", e.Type, strings.Join(parts, " ")))
			}
		},
		func(e *proto.RuntimeExceptionThrown) {
			d := e.ExceptionDetails
			msg := d.Text
			if d.Exception != nil {
				msg = d.Exception.Description
			}
			note("exception: " + msg)
		},
		func(e *proto.NetworkResponseReceived) {
			if e.Response.Status >= 400 {
				note(fmt.Sprintf("HTTP %d %s", e.Response.Status, e.Response.URL))
			}
		},
	)()

	for _, path := range flag.Args() {
		mu.Lock()
		issues = nil
		mu.Unlock()
		page.MustNavigate(*base + path).MustWaitStable()
		time.Sleep(500 * time.Millisecond)
		name := strings.Trim(strings.ReplaceAll(path, "/", "_"), "_")
		if name == "" {
			name = "root"
		}
		file := filepath.Join(*out, name+".png")
		page.MustScreenshotFullPage(file)
		mu.Lock()
		fmt.Printf("%s -> %s (%d issues)\n", path, file, len(issues))
		for _, s := range issues {
			fmt.Println("  " + s)
		}
		mu.Unlock()
	}
}
