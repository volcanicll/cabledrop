//go:build private_mac_apis

package serve

// translucentChrome is true when the build enables Wails' private macOS
// APIs: the panel webview turns transparent, so the page can draw a
// translucent tint and let the native vibrancy show through. Without the
// tag the webview is opaque and the pages keep their solid backgrounds.
const translucentChrome = true
