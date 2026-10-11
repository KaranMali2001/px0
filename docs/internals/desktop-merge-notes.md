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
| `web/style.css` | Traffic-light inset and drag regions on `#side header.panel-head` and `#tabs` (Wails v2 `--wails-draggable: drag`/`no-drag`, not `app-region`, which WKWebView ignores) | Keep, under `html.desktop` | Wails hidden titlebar; see `desktop-native-polish.md` |
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
| `server.go`, `web/src/settings.js`, `web/src/panels.js`, `web/style.css` | `workbench.sideBar.location` (left/right) re-added as a desktop-only setting | Keep (desktop-gated) | The user's `settings.json` uses `"right"`. `handleSettings` adds it to the schema/defaults only for trusted desktop requests (`desktopSettingsSchema`; `settings.go` stays byte-identical, `TestSidebarLocationSettingOnlyForDesktop`). `applySettingLive` ignores it unless `html.desktop` is present; the `body.side-right` layout is under `html.desktop`; the resizer mirrors its drag only when `side-right` is set, which the CLI never sets. Upstream sidebar width/visibility behaviour is unchanged |
| `README.md` | Native app section | Updated | Says ad-hoc signed (`make desktop` runs `codesign --sign -`) and that `make desktop` stamps `VERSION` into `Info.plist` while a bare `wails build` leaves 1.0.0 |
| `server.go` | `Server.Close()` (stops the git watcher and scavenger, `CloseThreads`, `agent.Close`, `lsp.Close`; idempotent) | Keep | Only the desktop app calls it; CLI shutdown in `main.go` is unchanged |

## Desktop startup

- `main()` (`desktop.go`) resolves and opens the startup workspace on a goroutine (`deskHandler.openAsync`), so `wails.Run` and the window never wait on `startupRoot`/`NewIndex`/`NewServer`/git work. The folder picker in `OnStartup` waits for that open to finish. A launch from a privacy-protected folder (e.g. `~/Desktop`) used to hang forever with no window because a child `git rev-parse` blocked before `wails.Run`.
- `deskHandler` (`desktop_handler.go`) holds webview requests until that open finishes (capped by `deskOpenWait`, 30s), so the unchanged web UI boots against a ready server. If a request gives up, it gets the existing 503 `no workspace open` and the window reloads once the open completes.
- Opening another folder swaps the active server (`deskHandler.install`) and closes the old one on a goroutine, so its git watcher, scavenger, LSP children, agent runs and thread turns stop. `OnShutdown` closes the active server (`deskHandler.shutdown`).
- `File > Open Folder…` resolves the picked folder with `resolveTarget` (abs + `EvalSymlinks`) before opening and remembering it, matching CLI/startup roots. `deskCtx` is an `atomic.Pointer` because menu callbacks and the startup goroutines read it while `OnStartup` writes it.
- The macOS About panel shows `px0 <version>` (from `version.go`/`VERSION`) and "Code navigator with agent editing".
- Git queries on the open path (`gitProbe`, `gitHeadCommit`) go through `gitQuery`, bounded by `gitQueryTimeout` (5s). A hung git is treated like a non-git folder for that call, but a timed-out probe is not cached in `gitCache`, so the next probe retries (e.g. after the user answers a TCC prompt).

## R2 review fixes

- N1: `gitQuery` returns `ctx.Err()` on timeout; `gitProbe` skips `gitCache` when either query hit `context.DeadlineExceeded` (`TestStartupGitQueriesAreBounded`).
- N2: `lspManager.Close` latches `closed`; `client`/`StartServer` return `errStopped` and a spawn finishing after Close shuts its child down (`TestManagerCloseLatches`).
- N7: `desktopIndex` also drops the two Google Fonts `preconnect` links, so the desktop page opens no connection to Google.
- N3: `handleEventStream` (`/api/stream`, `/api/git/stream`) and `handleThreadStream` return on `s.done`, so a closed server's streams stop ticking even when the request context never ends (Wails) (`TestStreamHandlersExitOnClose`).
- N9: `TestServerCloseIdempotentStopsWatcher` git-inits its root and waits on `GitWatcher.loopDone`, which the loop goroutine closes on exit; `make test-desktop` runs `go test -race -tags desktop ./...`.
- Q2: `desktopIndex` writes `<body class="side-right">` when `workbench.sideBar.location` is `right`, so right-sidebar users get no left-to-right relayout flash; the CLI index is unchanged (`TestIndexDesktopMarkerOnlyForDesktopRequests`).
- Item 9: the bundled fonts are the exact latin-subset files Google Fonts serves for upstream's css2 URL (JetBrains Mono v24): one upright variable file used for 400/500/600 and one 400 italic file, with Google's `unicode-range`. `OFL.txt` sits next to them in `web/static/fonts/`. The old 400/400i/500/700 files are gone. Upright came from a Safari UA request, italic from a Chrome UA request, because Safari is served the italic as woff.
- N4: `openFolder` (Open Folder… and Open Recent) waits for the startup open via `deskHandler.openAfterStartup`, so a slow startup install can no longer replace the folder the user picked. `install` after `shutdown` closes the new server instead of keeping it (mutex plus `closed` flag) (`TestDeskHandlerUserOpenWinsOverSlowStartup`, `TestDeskHandlerInstallAfterShutdownCloses`).
- N5: already fixed by slice 05 (0134e05, after fd4c088). With the sidebar hidden or on the right, the traffic lights sit in the 28px `#tabs` row, which is `--wails-draggable: drag` and stays visible even with no tabs open (`html.desktop #tabs:empty { display: flex; }`). Tabs, buttons, links and inputs are `no-drag`, and custom properties inherit, so the close `x` inside a tab is `no-drag` too (`web/style.css:3-6`). `#main` has no padding strip left to cover. Not verified at runtime: when tabs fill the whole row, the only drag area left is the 80px inset beside the traffic lights.

## Uncertain / follow-ups

- `.gitignore` from upstream ignores `build/`. The tracked `build/darwin/*` and `build/appicon.png` stay tracked, but new files there need `git add -f`.
- Desktop bundles only the latin subset, so non-latin code text falls back to system fonts where the web build would load Google's cyrillic/greek/vietnamese/latin-ext subsets.
