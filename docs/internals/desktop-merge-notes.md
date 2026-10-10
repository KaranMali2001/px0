# Desktop Merge Notes (upstream 02db0c7 into `desktop-parity`)

Merge of upstream `master` at `02db0c7` into `desktop-parity` (base `6be8a06` + desktop snapshot `b8e3012`). Upstream wins on shared web behaviour; the Wails desktop integration is kept and adapted.

## Conflicts and resolutions

| File | Resolution |
| --- | --- |
| `go.mod`, `go.sum` | Upstream module set (`github.com/px0-ai/harness`) plus `github.com/wailsapp/wails/v2 v2.16.0` and its indirect deps, re-tidied. `go` directive became `1.25.0` (from tidy). |
| `main.go` | Upstream `main.go` (PR URLs, `-base-path`, telemetry, timing) with `//go:build !desktop`, the `-d` flag, and the desktop branch re-added. `-d` with a PR URL is rejected. `version`, `resolveTarget`, `splitTargetLine` and `fatal` stay in `version.go` / `target.go` (they are identical to upstream's copies) so the desktop build still has them. |
| `server.go` | Kept both: upstream base-path routing (`registerRoutes`, `SetBasePath`) and `CloseThreads`, plus desktop `trustedDesktopRequest` / `isTrustedDesktopRequest`. The embed list now includes `web/static` (see below). |
| `highlight.go` | Upstream imports (`strconv`); desktop TypeScript method highlighting merged cleanly. |
| `settings_test.go` | Kept both `TestSettingsAcceptsTrustedDesktopRequest` and upstream `TestSettingsBasePath`. |
| `web/app.js` | Upstream no longer tracks the bundle (`.gitignore`). Removed from the index and regenerated with `node scripts/build-web.js`. |
| `Makefile` | Upstream targets plus desktop `desktop`, `dmg`, `install-cli`, `install-desktop`, `install-local`. `clean` does both: upstream's `web/app.js` reset and desktop's `build/bin/` removal. |
| `README.md` | Upstream README; the "Native macOS app" section moved under Installation. |
| `docs/internals/styling-and-themes.md` | Upstream text (localStorage `px0.theme`, default `catppuccin-mocha`), plus a note that the theme is mirrored to `workbench.colorTheme` in `settings.json`. |
| `web/index.html` | Upstream head (Google Fonts, relative `static/` URLs) plus the desktop `wails:` protocol class script. Superseded: see "Shared-file decisions". |
| `web/style.css` | Upstream rules, with upstream's 340px sidebar width. Desktop's removal of the `#side` background stays, so the translucent desktop material still shows. Font URLs are now `static/fonts/...`. |
| `web/src/state.js` | Upstream `request()` (base-relative URLs, JSON bodies) plus desktop's non-JSON / non-2xx error handling. Desktop `apiPostJSON` was dropped in favour of upstream `apiPostJson`. `persistPreference` was re-added on top of it. |
| `web/src/theme.js` | Upstream (localStorage + settings mirror, `theme:changed`). Desktop `chooseTheme` was dropped because upstream `setTheme` already persists to settings. |
| `web/src/settings.js` | Upstream (save status, `theme-preference`) plus the desktop sidebar location/width/visible settings and the rollback when a save fails. |
| `web/src/panels.js` | Upstream `showPanel(name)` / `reindexWorkspace` plus desktop `setSidebarLocation`, `setSidebarVisible`, `toggleSidebar`, the persisted width, and dragging when the sidebar is on the right. |
| `web/src/palette.js`, `web/src/shortcuts.js` | Upstream, with `Mod+B` routed through `toggleSidebar`. Palette theme preview uses `setTheme(id, false)` again. |
| `web/src/main.js` | Upstream boot (localStorage defaults, git sidebar state). Desktop's meta-poll backoff kept, with a `return` after ready. Settings loaded later still override localStorage. |
| `web/src/markdown.js` | Upstream table-aware `mdSetPref(d, on)`, also persisting to `markdown.preview.open` / `table.preview.open`. |
| `web/src/renderer.js` | Desktop row recycling (`render(force)`, `paint(force)`) kept, now emitting upstream's gutter markup with `.line-btn`. |
| `web/src/tabs.js` | Upstream `ref`/revision handling plus desktop `openFile` boolean result and `[px0 open]` console logging. |
| `web/src/tree.js` | Upstream markup (your-change, stage tick, row menu) with desktop SVG icons. The click handler runs upstream's `focusDiffScope`/`syncDiffView` around desktop's loading state. |
| `web/src/status.js` | Upstream (metrics arrive via the stream). Desktop focus-gated metrics polling was dropped as obsolete. |
| `web/src/diff.js`, `web/src/vim.js` | Import lists merged. Desktop's settings-based persistence merged cleanly. |

## Other changes made during the merge

- `workbench.sideBar.width` default moved from 228 to 340 (`settings.go`, `settings.js`, `settings_test.go`) to match upstream's sidebar CSS.
- `web/static` is now embedded. In `b8e3012` the `/static/` handler served `web/`, so `/static/favicon.ico` and `/static/fonts/*` did not resolve to `web/static/*`. Assets are now served at `static/static/...` (relative, so `-base-path` works too).
- Three upstream git tests (`TestPRReviewerChangesInIndex`, `TestGitStagedFilesStatAndDiff`, `TestDeletedFileHandling`) failed on macOS because `t.TempDir()` lives under the `/var` → `/private/var` symlink. They now resolve the temp dir the same way other tests in `git_test.go` already do.

## Shared-file decisions (parity with 02db0c7)

Desktop detection: `Server.handleIndex` calls `desktopIndex` only for requests marked by `trustedDesktopRequest` (set by the Wails `deskHandler`). It adds `class="desktop"` to `<html>`, so desktop-only CSS/JS keys off `html.desktop`. The CLI server serves `web/index.html` unchanged (`TestIndexDesktopMarkerOnlyForDesktopRequests`). The marker is a server-side attribute rewrite, not an inline script, so it works under the unchanged `script-src 'self'` CSP; the same test asserts the desktop page has no inline `<script>` and still carries that CSP.

| File | Change vs 02db0c7 | Decision | Reason |
| --- | --- | --- | --- |
| `web/index.html` | `wails:` class script; favicons/logos from `static/static/...` | Reverted to upstream; desktop rewrite moved to `desktopIndex` | Offline assets are desktop integration; CLI keeps upstream's remote URLs |
| `server.go` | `web/static` embed, `trustedDesktopRequest`, `desktopIndex` | Keep (desktop-gated) | Only reachable from the Wails handler; the extra embedded files are unused by the CLI page |
| `build/darwin/Info.plist`, `Info.dev.plist` | `NS{Desktop,Documents,Downloads}FolderUsageDescription`, `NS{Removable,Network}VolumesUsageDescription` | Added | macOS shows these when px0 reads a protected folder; bundle id unchanged |
| `web/src/tree.js` | SVG file-type/folder icons, special filename kinds, select-after-open with `.loading` | Reverted to upstream | Shared appearance/behaviour, not desktop-specific |
| `web/style.css` | Vendored `@font-face` rules | Moved to `web/static/fonts/fonts.css`; `desktopIndex` swaps the Google Fonts link for it | Offline fonts are desktop integration; CLI keeps upstream's Google Fonts |
| `web/style.css` | Traffic-light inset and drag region on `#side header.panel-head` (Wails v2 `--wails-draggable: drag`/`no-drag`, not `app-region`, which WKWebView ignores); `#main`/`#view-switches`/`#tabs` insets | Keep, under `html.desktop` | Wails hidden-inset titlebar |
| `web/style.css` | "Apple-native polish" (translucent status/sidebar, button press scale, modal transitions, tracking, reduced-motion/transparency), `.row { contain: content }`, tree icon/loading styles | Reverted to upstream | Shared appearance tweaks, not desktop integration |
| `web/style.css` | `body.side-right` layout and its `html.desktop` insets | Reverted to upstream | Part of the sidebar-location setting below |
| `settings.go`, `web/src/settings.js` | `workbench.sideBar.{location,width,visible}` settings | Reverted to upstream (`settings.go` byte-identical) | New shared settings, not desktop integration |
| `web/src/panels.js`, `web/src/palette.js`, `web/src/shortcuts.js` | `setSidebarLocation`/`setSidebarVisible`/`toggleSidebar`, persisted width, right-side dragging | Reverted to upstream (byte-identical) | Shared behaviour; went with the sidebar settings |
| `settings_test.go` | `TestSettingsAcceptsTrustedDesktopRequest` | Keep (sidebar assertions removed) | Tests the desktop trust path only |
| `web/src/{state,renderer,diff,vim,markdown,settings}.js` | `persistPreference` (word wrap, diff layout, vim mode, preview state saved to `settings.json` instead of localStorage); settings save rollback + toast | Reverted to upstream | Shared persistence behaviour, not desktop integration |
| `web/src/renderer.js` | Row recycling (`render(force)`/`paint(force)`), arithmetic caret placement | Reverted to upstream (byte-identical) | Shared rendering optimisation |
| `web/src/tabs.js` | `openFile` boolean result, `[px0 open]` console logging | Reverted to upstream (byte-identical) | Debug output; nothing used the result |
| `web/src/main.js` | Meta-poll exponential backoff | Reverted to upstream (byte-identical) | Shared behaviour |
| `web/src/state.js` | `request()` catches a non-JSON body and throws `<METHOD> <path> failed (<status>)` | Keep (upstream bug fix) | Upstream's `http.NotFound` and other plain-text replies made `r.json()` throw an opaque `SyntaxError`; JSON responses behave exactly as upstream |
| `highlight.go`, `highlight_test.go` | TypeScript method-declaration highlighting | Reverted to upstream (byte-identical) | Shared highlighting change |
| `lspsetup.go` | `localPost` accepts `isTrustedDesktopRequest` | Keep (desktop-gated) | The Wails origin is not `localhost`/an IP; CLI requests never carry the marker |
| `git.go`, `git_test.go` | `gitQuery` bounds `gitProbe`/`gitHeadCommit` with a 5s timeout; tests resolve `/var` → `/private/var` | Keep (bug fixes) | A child `git rev-parse` can block forever (macOS TCC); upstream tests fail on macOS temp dirs |
| `main.go`, `version.go`, `target.go`, `desktop*.go`, `webview_fps_darwin.go` | `!desktop` build tag, `-d` flag, helpers split out for the desktop build | Keep (desktop integration) | `-d` only launches the app; other CLI paths are upstream's code |
| `main.go` → `listen.go` | `viewerURL`, `networkURLs`, `networkURLsFromAddrs`, `listen` moved unchanged into an untagged file | Keep (move only) | `px0_test.go` uses them, so `go vet/test -tags desktop ./...` now compile and pass |
| `server.go` | `Server.Close()` (stops the git watcher and scavenger, `CloseThreads`, `agent.Close`, `lsp.Close`; idempotent) | Keep | Only the desktop app calls it; CLI shutdown in `main.go` is unchanged |

## Desktop startup

- `main()` (`desktop.go`) resolves and opens the startup workspace on a goroutine (`deskHandler.openAsync`), so `wails.Run` and the window never wait on `startupRoot`/`NewIndex`/`NewServer`/git work. The folder picker in `OnStartup` waits for that open to finish. A launch from a privacy-protected folder (e.g. `~/Desktop`) used to hang forever with no window because a child `git rev-parse` blocked before `wails.Run`.
- `deskHandler` (`desktop_handler.go`) holds webview requests until that open finishes (capped by `deskOpenWait`, 30s), so the unchanged web UI boots against a ready server. If a request gives up, it gets the existing 503 `no workspace open` and the window reloads once the open completes.
- Opening another folder swaps the active server (`deskHandler.install`) and closes the old one on a goroutine, so its git watcher, scavenger, LSP children, agent runs and thread turns stop. `OnShutdown` closes the active server (`deskHandler.shutdown`).
- `File > Open Folder…` resolves the picked folder with `resolveTarget` (abs + `EvalSymlinks`) before opening and remembering it, matching CLI/startup roots. `deskCtx` is an `atomic.Pointer` because menu callbacks and the startup goroutines read it while `OnStartup` writes it.
- The macOS About panel shows `px0 <version>` (from `version.go`/`VERSION`) and "Code navigator with agent editing".
- Git queries on the open path (`gitProbe`, `gitHeadCommit`) go through `gitQuery`, bounded by `gitQueryTimeout` (5s). A hung git is treated like a non-git folder.

## Uncertain / follow-ups

- `.gitignore` from upstream ignores `build/`. The tracked `build/darwin/*` and `build/appicon.png` stay tracked, but new files there need `git add -f`.
- Desktop loads only the bundled JetBrains Mono (400, 400 italic, 500, 700); upstream's Google Fonts request asks for 600 instead of 700, so desktop semibold text uses the nearest bundled weight.
