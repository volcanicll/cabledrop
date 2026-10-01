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
