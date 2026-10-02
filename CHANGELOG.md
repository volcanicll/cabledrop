# Changelog

All notable changes to CableDrop are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versioning is
[SemVer](https://semver.org/).

## 0.0.1 - 2026-10-02

First release.

### Added

- Two-way file transfer between the desktop and an Android phone over one
  USB cable, with the shared folder on the desktop browsable from the phone,
  subfolders included
- Clipboard bridge in both directions: the desktop clipboard is published to
  the phone's browser (Clipboard API over `adb reverse`), text typed on the
  phone lands in the desktop clipboard, and a dedicated note slot hands text
  from the desktop to the phone
- Menu-bar panel on macOS (NSPanel with vibrancy, native Escape/focus
  dismissal), tray-driven panel on Windows and Linux
- Live transfer progress: byte-weighted percentage for multi-file batches,
  elapsed time for single adb transfers, real percentage for phone uploads
- Device detection over a persistent `adb track-devices` connection, with a
  low-frequency polling fallback
- Android APK (optional WebView shell) with system file picking, Downloads
  integration and a clear offline state
- Icons drawn in code by a small in-process rasteriser; no binary artwork in
  the repository

## Unreleased

### Changed

- Panel UI polish pass: the phone's real model name is the header headline
  (asked from the device over `getprop` when adb's listing has none), the
  connection line spells out "USB 已连接 · 77 GB 可用", and a small
  Mac ⇄ USB ⇄ Android diagram lights up with the link
- Action cards carry a clearer hierarchy: 发送文件 is a visible drop target
  that highlights on hover-drag, 手机文件 and 剪贴板 gain direction and
  capability hints, and 手机网页 shows the real address with a copy button
  instead of using localhost as the tile's subtitle
- 共享目录 is now 文件保存位置 (path on its own line, with 打开/更改 as
  outlined buttons); 最近传输 rows show direction, destination, size,
  relative time and a status badge, collapse to the newest three, and expand
  on demand
- Transfer failures and device errors are translated into actionable
  language with the adb text demoted to a secondary line; a green trust
  banner states the USB-only guarantee without overclaiming
- Pulls record the file size once the local copy exists, so their history
  rows show a size like pushes do

- Second polish pass. The panel no longer scrolls in its worst case: the
  error card is tighter and, while it is up, 最近传输 shows two rows instead
  of three, so the fixed 380×540 window holds every state without a scroll
  bar. Transfer rows gain the reference's chevron and act as a disclosure —
  tapping one reveals the full file name or the full failure detail that the
  one-line layout had to truncate. The 手机网页 tile gets the same chevron the
  other cards carry.
- The phone page adopts the panel's design language: a connection dot and a
  Mac ⇄ Android header motif, a green USB-only trust strip, icons on each
  section label, and a proper 未连接到电脑 banner with a 重试 button that
  replaces the old bare subtitle. Copy now reports failure instead of always
  claiming success, and an empty copy is called out.
- The Android shell follows the system dark mode: a DayNight theme and a
  window background that match the page's own canvas (no white flash on cold
  start), night colours for the offline screen, and a live re-tint when the
  user flips the theme without the activity being recreated

### Added

- `site/` — a dependency-free product landing page (one HTML file, one
  stylesheet, inline SVG icons, no framework and no build step) that reuses
  the app's own colour tokens so the page and the product look like the same
  thing, plus an optional-Android-app section with a real device screenshot
- Release and Pages workflows. `release.yml` builds every installable artifact
  — a universal macOS `.app` and `.dmg`, both Windows architectures
  cross-compiled, a Linux tarball, and the APK — smoke-tests each one, and
  attaches them to a GitHub Release with checksums on a `v*` tag. `site.yml`
  stages the landing page with the screenshots it references and publishes it
  to GitHub Pages. `make app-universal`, `make dmg` and `make version` exist so
  a release build and a local build are the same commands

### Fixed

- The phone page was laid out for a current browser and the Android build does
  not get one. The APK renders in the ROM's own WebView, which on the phone
  this was tested against is an AOSP build pinned at Chrome 75 — and an engine
  that does not understand a declaration does not complain, it drops it and
  paints something plausible. Three failures came out of that, all silent:
  `inset: 0` was dropped, so the fixed body fell back to shrink-to-fit and the
  page occupied a 328px strip of a 432px viewport; flex `gap` was parsed and
  then ignored, so every row lost its spacing at once; and `max()` inside a
  `padding` shorthand took the page's entire gutter with it. `base.css` and
  `phone.css` now stay inside what these engines can do — offsets spelled out,
  `100vh` before `100dvh`, margins instead of flex `gap`, and tokens instead of
  `color-mix()` — with `TestPhoneCSSWorksOnOldEngines` failing the build if any
  of it comes back. Note that a fallback written *above* a modern declaration
  only works when the modern value has no `var()`/`env()` in it: with one, the
  old engine keeps the declaration, fails to compute it, and `unset` discards
  the fallback too, so those have to sit behind `@supports`
- The device name in the header was a model code. `deviceMarketName` asked for
  four properties and this ROM fills none of the first three, so the panel and
  the phone page both read "DT2002C"; `net.devicename`, which carries the name
  the device calls itself, is now consulted before the model codes
- The Android shell painted a dark window frame around a light page. The page
  cannot honour `prefers-color-scheme` on an engine below Chrome 76, so the
  frame now follows what the page can actually do rather than what the system
  asked for
- The phone page rendered the desktop panel's type scale. `tokens.css` is sized
  for a 380x540 window — a 13px base and an 11.5px small — and the phone page
  inherited it wholesale, which is the difference between an app and a shrunken
  panel: 11.5px secondary text is comfortable on a monitor and small in a hand.
  The phone page now restates the scale for its own subtree (15px base, 14.5px
  body, 13px secondary), which is the whole fix, because every rule already
  reads the same variable names. Touch targets moved with it: 48px buttons and
  56px list rows
- `scripts/shots.py` could silently drive the wrong browser. It asked Chrome for
  a fixed DevTools port, and `adb forward tcp:9333` — the thing you set up to
  inspect a phone's WebView — takes the same number, so Chrome failed to bind
  and the readiness poll answered from the phone instead. Every screenshot came
  back wrong with no error anywhere. The port is now allocated per run, the
  Chrome profile is a fresh temp directory (a killed run used to leave a profile
  whose every navigation landed on `chrome-error://chromewebdata`), and the
  capture asserts the page it got is the page it asked for
- The `docs/` screenshots were incomplete, and the script that generated them
  could not have produced anything else. Both pages are `100dvh` roots around
  an inner scroller, so a plain `--screenshot` only ever captured the viewport;
  enlarging `--window-size` did not fix it but moved the cut, because the inner
  layout is viewport-dependent and a taller window just re-laid the page out at
  a size no device has (past ~1300px the surplus came back as a blank tail).
  `scripts/shots.py` now releases the inner scrollers at capture time and
  screenshots past the viewport, so each image is the whole page at a viewport
  the layout was designed for. It also drops the sips/BMP trim round-trip and
  the third-party WebSocket dependency: the DevTools client is stdlib.
- The generated artwork was cropped and off-centre at most sizes. The
  downsampler divided the buffer side by the output side in integer maths, so
  every size that was not a divisor of the 256-sample canvas rounded down to a
  ratio of 1 and read the buffer's top-left corner instead of the whole thing —
  which cut the right and bottom edges off the rounded corners and shifted the
  icon. Android's 48/72/96/144/192 launcher densities were all affected, as was
  the 48px bitmap that feeds the Windows icon. `mask.coverage` is now an
  area-weighted box filter over each output pixel's real footprint, and
  `TestAppIconGeometry` fails if any shipped size is not centred or touches the
  canvas edge.

