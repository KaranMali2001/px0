//go:build !desktop

// Package main implements px0: a fast, local-first code navigator and review tool
// that opens any repository or pull request in a responsive browser UI.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func main() {
	var (
		port         = flag.Int("port", 7777, "port to listen on (0 picks a free one)")
		host         = flag.String("host", "127.0.0.1", "address to bind")
		noOpen       = flag.Bool("no-open", false, "do not launch a browser")
		desktop      = flag.Bool("d", false, "open a directory in the native macOS app")
		noLSP        = flag.Bool("no-lsp", false, "do not use language servers, even if installed")
		noGit        = flag.Bool("no-git", false, "disable git awareness")
		showVer      = flag.Bool("version", false, "print version and exit")
		showVerShort = flag.Bool("v", false, "print version and exit (shorthand)")
		doUpdate     = flag.Bool("update", false, "check for and install latest version of px0")
		noUpdate     = flag.Bool("no-update", false, "do not auto-update px0 on startup")
		noColor      = flag.Bool("no-color", false, "disable colour output")
		quiet        = flag.Bool("quiet", false, "suppress narration")
		verbose      = flag.Bool("verbose", false, "log startup steps, requests, searches, symbols, and agent prompts to terminal")
		noTelemetry  = flag.Bool("no-telemetry", false, "disable anonymous usage telemetry")
		agentCmd     = flag.String("agent", "", "pin the coding harness used for edits (claude, gemini, cursor-agent, agy, opencode, codex, aider, goose, or a command template containing {prompt}); detected and chosen in the UI when omitted")
		noAgent      = flag.Bool("no-agent", false, "do not offer editing through a coding harness")
		_            = flag.Bool("y", false, "answer yes to prompts (deprecated; PRs are always opened without prompt)")
		_            = flag.Bool("yes", false, "answer yes to prompts (alias for -y)")
		basePathFlag = flag.String("base-path", "", "base URL path prefix to serve endpoints and assets from (e.g. /rev-123/)")
	)
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "px0 %s - a code navigator\n\nusage:\n  px0 [flags] [file or directory]\n  px0 [flags] <pr-url>\n\nflags:\n", version)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *noColor {
		f := false
		uiForcedColor = &f
	}
	if *quiet {
		uiQuiet = true
	}
	if *verbose {
		uiVerbose = true
	}
	if *noGit {
		gitDisabled = true
	}

	if *showVer || *showVerShort || (flag.NArg() == 1 && flag.Arg(0) == "version") {
		fmt.Printf("px0 %s (%s/%s)\n", version, runtime.GOOS, runtime.GOARCH)
		return
	}

	if *doUpdate {
		if err := runSelfUpdate(version); err != nil {
			fatal(err)
		}
		return
	}

	// A full pull request URL (e.g. https://github.com/owner/repo/pull/123)
	// checks out the PR's full source tree instead of resolving a local file/directory.
	// Only full URLs via "px0 <url>" are supported for PR review.
	target := "."
	var prProvider GitProvider
	var prTarget PRTarget
	isPR := false
	if flag.NArg() > 0 {
		arg0 := flag.Arg(0)
		if arg0 == "pr" {
			fatal(fmt.Errorf("'px0 pr' is no longer supported; open pull requests directly with: px0 <url>"))
		}
		if provider, pt, ok := DetectPRURL(arg0); ok {
			prProvider, prTarget, isPR = provider, pt, true
		} else {
			target = arg0
		}
	}
	if *desktop {
		if isPR {
			fatal(fmt.Errorf("-d does not support pull request URLs"))
		}
		root, err := desktopWorkspace(target)
		if err != nil {
			fatal(err)
		}
		if err := launchDesktop(root); err != nil {
			fatal(err)
		}
		return
	}
	if isPR && gitDisabled {
		fatal(fmt.Errorf("px0: git is required for PR review; remove -no-git"))
	}

	var pr *prSession
	var root, initialFile string
	var initialLine int
	var targetDur time.Duration
	if isPR {
		sp := newSpinner(fmt.Sprintf("Preparing PR #%d (%s/%s)...", prTarget.Number, prTarget.Owner, prTarget.Repo), os.Stdout)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		p, err := checkoutPR(ctx, prProvider, prTarget, ".", func(msg string) {
			sp.Update(msg)
		})
		cancel()
		if err != nil {
			sp.Fail(fmt.Sprintf("Failed to prepare PR #%d: %v", prTarget.Number, err))
			fatal(fmt.Errorf("px0: %w", err))
		}
		if p.meta.Merged {
			sp.Success(fmt.Sprintf("PR #%d checked out [merged] (%s)", prTarget.Number, p.meta.Title))
		} else {
			sp.Success(fmt.Sprintf("PR #%d checked out (%s)", prTarget.Number, p.meta.Title))
		}
		pr = p
		root = p.Root()
	} else {
		tStart := time.Now()
		r, f, l, err := resolveTarget(target)
		if err != nil {
			fatal(err)
		}
		targetDur = time.Since(tStart)
		root, initialFile, initialLine = r, f, l
	}

	tListen := time.Now()
	ln, addr, err := listen(*host, *port)
	if err != nil {
		fatal(err)
	}
	listenDur := time.Since(tListen)

	tInit := time.Now()
	ix := NewIndex(root)
	lsp := newLSPManager(root, !*noLSP)
	tel := NewTelemetryService(*noTelemetry)
	defer tel.Close("normal")

	configuredBasePath := "/"
	if *basePathFlag != "" {
		configuredBasePath = cleanBasePath(*basePathFlag)
	} else if cfg := readSettings(); cfg.ServerBasePath != nil && *cfg.ServerBasePath != "" {
		configuredBasePath = cleanBasePath(*cfg.ServerBasePath)
	}

	pxSrv := NewServer(ix, lsp, configuredBasePath)
	pxSrv.tel = tel
	if pr != nil {
		pxSrv.SetPR(pr)
	}
	var agent *agentManager
	if !*noAgent {
		agent, err = newAgentManager(root, *agentCmd, lsp)
		if err != nil {
			fatal(fmt.Errorf("-agent: %w", err))
		}
		pxSrv.SetAgent(agent)
	}
	initDur := time.Since(tInit)

	srv := &http.Server{Handler: pxSrv}

	url := viewerURL(addr, initialFile, initialLine, configuredBasePath)
	uiHeading("px0 "+version, nil, os.Stdout)
	if pr != nil {
		prTitle := fmt.Sprintf("#%d %s", pr.meta.Number, pr.meta.Title)
		if pr.meta.Merged {
			prTitle += " " + paint("[MERGED]", colorWarn, true, os.Stdout)
		}
		uiKV("PR", prTitle, 11, os.Stdout)
		if pr.token == "" {
			uiKV("access", uiDim(fmt.Sprintf("read-only (no %s token: set GITHUB_TOKEN or gh auth login to submit reviews)", pr.provider.Name()), os.Stdout), 11, os.Stdout)
		}
	}
	uiKV("workspace", root, 11, os.Stdout)
	uiKV("url", uiAccent(url, os.Stdout), 11, os.Stdout)
	if *host == "0.0.0.0" {
		for _, networkURL := range networkURLs(addr, initialFile, initialLine) {
			uiKV("network", uiAccent(networkURL, os.Stdout), 11, os.Stdout)
		}
	}
	uiHint("ctrl-c to stop", os.Stdout)

	if uiVerbose {
		if !isPR {
			uiStatus("ok", "resolved workspace target", fmtDuration(targetDur), 0, os.Stdout)
		}
		uiStatus("ok", fmt.Sprintf("bound TCP listener on %s", addr), fmtDuration(listenDur), 0, os.Stdout)
		uiStatus("ok", "initialized HTTP server and services", fmtDuration(initDur), 0, os.Stdout)
	}

	// Launch browser immediately without blocking startup.
	if !*noOpen {
		tBrowser := time.Now()
		go openBrowser(url)
		if uiVerbose {
			uiStatus("ok", "spawned browser launcher", fmtDuration(time.Since(tBrowser)), 0, os.Stdout)
		}
	}

	// Index workspace asynchronously so the server and UI respond in <1ms.
	go func() {
		ix.Build()
		n, _, ms := ix.Stats()
		uiStatus("ok", fmt.Sprintf("indexed %d files", n), fmt.Sprintf("%dms", ms), 0, os.Stdout)
		tLSP := time.Now()
		names := lsp.Available()
		lspDur := time.Since(tLSP)
		if len(names) > 0 {
			if uiVerbose {
				uiStatus("ok", fmt.Sprintf("discovered language servers: %s", strings.Join(names, ", ")), fmtDuration(lspDur), 0, os.Stdout)
			} else {
				uiBullet(fmt.Sprintf("language servers: %s (started on first use)", strings.Join(names, ", ")), os.Stdout)
			}
		} else if uiVerbose {
			uiStatus("ok", "checked language servers (none installed)", fmtDuration(lspDur), 0, os.Stdout)
		}
		if agent != nil {
			tAgent := time.Now()
			var found []string
			for _, h := range agent.Detect() {
				if h.Installed {
					item := h.Name
					if h.Model != "" {
						item = fmt.Sprintf("%s (%s)", h.Name, h.Model)
					}
					found = append(found, item)
				}
			}
			agentDur := time.Since(tAgent)
			if uiVerbose {
				if len(found) > 0 {
					uiStatus("ok", fmt.Sprintf("detected coding harnesses: %s", strings.Join(found, ", ")), fmtDuration(agentDur), 0, os.Stdout)
				} else {
					uiStatus("ok", "checked coding harnesses (none found)", fmtDuration(agentDur), 0, os.Stdout)
				}
			}
		}

		tel.Track("session_started", map[string]any{
			"files_bucket": filesBucket(n),
			"index_ms":     ms,
			"has_git":      gitAvailable(root),
			"has_lsp":      len(lsp.Available()) > 0,
		})
	}()

	// Check for updates in the background once the server is already up, so
	// px0 never makes a network call before serving the first request.
	if !*noUpdate {
		go autoUpdate(version)
	}

	// Language servers are children that can hold gigabytes. Shut them down on
	// the way out rather than leaving them for the OS to reap.
	interrupted := false
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		interrupted = true
		fmt.Print("\r")
		uiStatus("info", "px0 stopped", "", 0, os.Stderr)
		go func() {
			<-stop // Second interrupt forces immediate exit
			os.Exit(130)
		}()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	err = srv.Serve(ln)
	lsp.Close()
	agent.Close()
	pxSrv.CloseThreads()
	pr.Close()

	if interrupted {
		tel.Close("interrupted")
		os.Exit(130)
	}

	tel.Close("normal")
	if err != nil && err != http.ErrServerClosed {
		fatal(err)
	}
}

func openBrowser(url string) {
	// If BROWSER environment variable is set, try that first
	if b := os.Getenv("BROWSER"); b != "" {
		if cmd := exec.Command(b, url); cmd.Start() == nil {
			return
		}
	}

	var cmds []*exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmds = []*exec.Cmd{exec.Command("open", url)}
	case "windows":
		cmds = []*exec.Cmd{
			exec.Command("rundll32", "url.dll,FileProtocolHandler", url),
			exec.Command("cmd.exe", "/c", "start", url),
		}
	default:
		// On Linux/Unix, detect WSL to open the browser on the Windows host seamlessly
		if isWSL() {
			// WSL2's localhost port-forwarding relay needs a moment to notice a
			// freshly bound listener before Windows can reach it. Without this,
			// the browser can open before the relay catches up and shows a
			// connection error until the next reload.
			time.Sleep(500 * time.Millisecond)
			cmds = append(cmds,
				exec.Command("wslview", url),
				exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "Start-Process", fmt.Sprintf(`"%s"`, url)),
				exec.Command("cmd.exe", "/c", "start", "", url),
			)
		}

		// On Linux/Unix desktop, try xdg-open, sensible-browser, gio, or common browsers
		cmds = append(cmds,
			exec.Command("xdg-open", url),
			exec.Command("sensible-browser", url),
			exec.Command("gio", "open", url),
			exec.Command("google-chrome", url),
			exec.Command("firefox", url),
			exec.Command("chromium", url),
		)
	}

	for _, cmd := range cmds {
		if err := cmd.Start(); err == nil {
			return
		}
	}
}

func isWSL() bool {
	if os.Getenv("WSL_DISTRO_NAME") != "" || os.Getenv("WSL_INTEROP") != "" {
		return true
	}
	data, err := os.ReadFile("/proc/version")
	if err == nil && (strings.Contains(strings.ToLower(string(data)), "microsoft") || strings.Contains(strings.ToLower(string(data)), "wsl")) {
		return true
	}
	return false
}
