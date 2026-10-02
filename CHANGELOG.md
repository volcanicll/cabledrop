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
