# Contributing to CableDrop

Thanks for helping. This document covers the rules that keep the project
maintainable — a few of them exist because of bugs that were genuinely painful
to find, and those are marked **hard constraint**.

## Setup

```bash
git clone https://github.com/volcanicll/cabledrop
cd cabledrop
make build   # native binary for this machine
make test    # go vet ./... && go test ./...
```

There is no npm, no node_modules, no frontend build step. The frontend is
plain HTML/CSS/JS under `internal/serve/assets`, embedded with `go:embed`.

## Hard constraints

These are not style preferences. They protect against failure modes that cost
real debugging time.

### 1. Never use Wails' built-in file dialogs

`application.Dialog.OpenFile()` and friends are banned. The macOS
implementation returns a channel that the caller ranges over while the main
thread sends paths into it over an unbuffered send; a miss in its internal map
panics, a dismissed dialog can close the channel before the send, and the
whole chain sits behind a synchronous main-thread dispatch. Any of those
leaves the request handler waiting forever and the main thread wedged — the
UI freezes while the HTTP server keeps answering, which is the worst kind of
bug to find.

Use `internal/picker` (osascript / PowerShell / zenity as a subprocess) and
its `beginModal`/`endModal` dance so the always-on-top panel does not cover
the dialog.

### 2. Never let a Wails callback panic, and never block the main thread

`InvokeSync` runs a closure on the main thread and waits for it with a
WaitGroup. A panic inside the closure skips `wg.Done()`, and the caller blocks
forever. Corollaries:

- No `InvokeSync`-backed APIs (including `SetSize`) anywhere on an HTTP
  request path.
- UI work from background goroutines goes through `InvokeAsync` (see
  `internal/ui`).
- Window geometry is fixed at creation. The panel never resizes itself at
  runtime.

### 3. Every device path goes through the whitelist

`device.CheckDevicePath` allows only `/sdcard` and `/storage` (resolved, so
`..` cannot climb out). `adb` can read the whole phone filesystem, and paths
arrive from a web page — without the check, `/api/device/pull` would copy
`/etc/passwd` into the shared folder, which is then served to the phone. New
endpoints that accept a device path must call `CheckDevicePath` synchronously
and answer 403 before starting any background work.

### 4. Don't fork adb on a timer

`adb` is a 19 MB binary; polling it every two seconds costs measurable CPU.
Device presence comes from the persistent `adb track-devices` stream
(`device.ADB.TrackDevices`) with a low-frequency fallback poll. The same
applies to the tray icon: only touch it when something it displays changed
(`trayNeedsUpdate`).

## Architecture

```
main.go            flag dispatch and assembly only
internal/model     shared data types, zero dependencies
internal/device    adb wrapper, device file ops, path whitelist
internal/clipboard desktop clipboard via platform commands
internal/picker    system file dialogs as subprocesses
internal/serve     HTTP API + embedded assets; depends on model + Backend
internal/app       state machine; implements serve.Backend
internal/icon      code-drawn icons + rasteriser
internal/ui        the only package that talks to Wails
```

`serve` defines the narrow `Backend` interface it needs; `app` implements it
and starts the phone-facing listener. `ui` implements `app.PanelUI`/`TrayUI`.
New features should keep those dependency directions.

## Tests

`make test` must pass before every commit. UI behaviour is verified against
the dev server (`CABLEDROP_DEVSERVER=1 go test -run TestDevServer
-timeout 1h ./internal/serve/`) with real DOM checks — `clientWidth` vs
`scrollWidth` for truncation, scroll reachability, hit-target sizes. Screenshots
alone are not evidence: visual models misreport truncation both ways.

Device, clipboard and transfer behaviour are tested through injected fakes
(`ADB.runHook`, `Clipboard.read/write`, `App.pushFile/pullFile/trackDevices`);
extend those seams rather than reaching for the real adb in tests.

## Conventions

- In-app UI copy stays **Chinese**; there is no i18n framework. Keep the
  existing tone: short, concrete, no exclamation marks.
- Icons are drawn in code (`internal/icon`); do not add binary artwork.
- Commits: one logical change per commit, imperative subject line.
- The Android APK lives under `android/` and uses only platform APIs — no
  AndroidX, no third-party dependencies.
