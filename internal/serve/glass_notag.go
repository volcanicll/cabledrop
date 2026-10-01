//go:build !private_mac_apis

package serve

// See glass_tag.go: without the private-APIs tag the webview stays opaque.
const translucentChrome = false
