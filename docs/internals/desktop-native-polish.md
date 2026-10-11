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

## Known limits

- In full screen the lights are hidden but the 80px inset (sidebar right/hidden) remains; Wails v2 exposes no full-screen CSS hook.
