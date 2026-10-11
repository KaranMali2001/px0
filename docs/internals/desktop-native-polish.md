# Desktop Native Polish

Intentional differences between the macOS desktop app and the upstream web UI (`02db0c7`). Every rule here is scoped to `html.desktop` (set server-side by `desktopIndex` for trusted Wails requests only), so the CLI web UI renders exactly as upstream.

| What | Why | Guideline | How to disable |
| --- | --- | --- | --- |
| Title bar is `mac.TitleBarHidden()` (standard 28px), not `TitleBarHiddenInset()` | The inset variant centres the traffic lights at ~26px, which forced a ~28px empty band above the 28px tab bar. With the standard height the lights sit in a 28px row that the tab bar and sidebar header can share | apple-design: content-first, no dead chrome; macOS unified title bar/toolbar row | Chrome accommodation, always on (not affected by `desktop.nativeStyling`) |
| Sidebar header shares the traffic-light row (sidebar left): `height: 28px`, `80px` left inset | Only what is actually under the lights is inset; the main column starts at the window top | better-layout: no wasted vertical space; alignment with the lights row | Chrome accommodation, always on |
| Sidebar right or hidden: tab bar gets an `80px` left inset instead | The lights then sit over the tab bar | same | Chrome accommodation, always on |
| Tab bar is a drag region (`--wails-draggable: drag`); tabs, buttons, links and inputs in it and in the sidebar header are `no-drag` | Empty title-bar areas move the window like any native toolbar; controls keep working | apple-design: native window behaviour | Chrome accommodation, always on |
| Empty tab bar stays visible (`#tabs:empty { display: flex }`) | Without open tabs there would be no drag strip over the main column and the lights would overlap content when the sidebar is right/hidden | same | Chrome accommodation, always on |

## `desktop.nativeStyling`

Desktop-only boolean setting (default `true`, listed in the settings UI only for trusted desktop requests via `desktopSettingsSchema`). When on, `desktopIndex` renders `<html class="desktop native">`, and toggling it in settings flips `html.native` live. Every refinement that is not chrome accommodation is scoped to `html.desktop.native`, so turning it off leaves the desktop app identical to the upstream web UI apart from the rows above marked "Chrome accommodation".

## Native shell (Go/Wails, `desktop.go`)

Not affected by `desktop.nativeStyling`: these are native app behaviour, not web styling.

| What | Why | Guideline | How to disable |
| --- | --- | --- | --- |
| App menu: Wails `AppMenu` role (About px0 &lt;version&gt;, Hide, Hide Others, Show All, Quit) plus a native **Settings… ⌘,** item inserted by `desktop_menu_darwin.go` | Wails' role has no Settings slot; inserting a real `NSMenuItem` keeps the native About panel and Hide/Quit selectors instead of re-implementing them. Re-inserted after every menu rebuild (tagged, idempotent) | apple-design: standard macOS app menu layout | Not configurable |
| File: Open Folder… ⌘O, Open Recent (last 10 folders, missing ones dropped), Close Tab ⌘W | Recent list reuses the `last-folder` state file (now newest-first, one per line; old one-line files still load). The menu is rebuilt after each open | apple-design: familiar document-app menus | Not configurable |
| Edit: Wails `EditMenu` role; Window: Wails `WindowMenu` role (Minimize ⌘M, Zoom, Full Screen ⌃⌘F) | Standard responder-chain items; native clipboard/undo in text fields | same | Not configurable |
| View: Toggle Sidebar ⌘B, Reload ⌘R, Toggle Full Screen; Help: px0 on GitHub | Menu discoverability for existing shortcuts | better-accessibility: commands reachable without memorised shortcuts | Not configurable |
| Menu items for web commands (Settings, Close Tab, Toggle Sidebar) emit the Wails event `px0:menu`; `shortcuts.js` handles it only under `html.desktop` by calling the same functions as the keyboard path | Native menu accelerators win over page `keydown`, so the page must receive the command; no logic is duplicated in Go | emil-design-eng: one source of truth per action | n/a |
| Window size and position saved on close (only when the window is normal, not full screen/maximised) to `window.json` next to `settings.json`, restored at launch; min size stays 600×400 | Native apps reopen where you left them | apple-design: spatial consistency | Delete `window.json` |
| Native chrome follows system appearance (`mac.DefaultAppearance`) | Menus, dialogs and traffic lights match the OS light/dark setting | apple-design | Not configurable |

## Known limits

- Open Recent shows full paths; a moved display can leave a restored window partly off-screen (macOS does not clamp `WindowSetPosition`).
- In full screen the lights are hidden but the 80px inset (sidebar right/hidden) remains; Wails v2 exposes no full-screen CSS hook.
