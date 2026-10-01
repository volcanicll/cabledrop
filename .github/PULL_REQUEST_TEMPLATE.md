## What and why

<!-- One paragraph: the change and the reason for it. -->

## How it was verified

<!-- go test ./... and go vet ./... are assumed. Beyond that: what did you
     actually run, click or measure? For UI changes, name the DOM checks
     (overflow, scroll reachability, hit targets), not just screenshots. -->

## Constraints checklist

- [ ] No Wails file dialogs (internal/picker instead)
- [ ] No InvokeSync / SetSize on request paths
- [ ] Device paths pass device.CheckDevicePath
- [ ] No new fork-every-N-seconds patterns
- [ ] In-app copy stays Chinese; no binary artwork added
