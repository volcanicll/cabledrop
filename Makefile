# CableDrop — build and packaging.
#
# The desktop UI links the platform's own webview, which on macOS and Linux
# goes through cgo, so those two are built natively. Windows uses WebView2
# through pure syscalls, so both its architectures cross-compile from a Mac.
# In practice that means macOS and Windows releases are both produced on a
# developer's Mac, and Linux needs a Linux machine.

BINARY   = cabledrop
APPNAME  = CableDrop
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
WINVER   = $(shell echo $(VERSION) | sed -E 's/^v//; s/[^0-9.].*//')

LDFLAGS  = -s -w -X main.version=$(VERSION)
# A checkout whose ownership git won't vouch for (a CI mount, a shared folder)
# fails the build with "error obtaining VCS status"; stamping is not worth that.
VCS      = -buildvcs=false
TAGS     = production

# macOS 12 is the floor for the Go toolchain; Info.plist says the same.
ifeq ($(shell uname -s),Darwin)
  export CGO_CFLAGS  = -mmacosx-version-min=12.0
  export CGO_LDFLAGS = -mmacosx-version-min=12.0
endif

# Older Linux distributions ship GTK 3 and WebKitGTK 4.1, not the GTK 4 /
# WebKitGTK 6 that Wails defaults to.
ifeq ($(shell uname -s),Linux)
  TAGS += gtk3
endif

.PHONY: all build run app windows linux icons icons-android apk dist release test vet fmt clean

all: build

## Native binary for this machine.
build:
	go build $(VCS) -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" -o $(BINARY) .

run: build
	./$(BINARY)

## macOS bundle: a menu bar app with no Dock icon (LSUIElement).
app: build icons
	@rm -rf $(APPNAME).app
	@mkdir -p $(APPNAME).app/Contents/MacOS $(APPNAME).app/Contents/Resources
	@cp $(BINARY) $(APPNAME).app/Contents/MacOS/$(BINARY)
	@cp build/darwin/AppIcon.icns $(APPNAME).app/Contents/Resources/AppIcon.icns
	@sed 's/@VERSION@/$(VERSION)/' build/darwin/Info.plist > $(APPNAME).app/Contents/Info.plist
	@codesign --force --deep --sign - $(APPNAME).app >/dev/null 2>&1 || true
	@echo "  $(APPNAME).app"

## Windows executables, cross-compiled. No cgo, so this works from macOS.
windows: icons
	@mkdir -p dist
	@go run github.com/tc-hib/go-winres@v0.3.3 make --in build/windows/winres.json \
		--arch amd64,arm64 --out rsrc \
		--file-version "$(WINVER)" --product-version "$(WINVER)"
	@for arch in amd64 arm64; do \
		echo "  windows/$$arch"; \
		CGO_ENABLED=0 GOOS=windows GOARCH=$$arch go build $(VCS) -tags "$(TAGS)" -trimpath \
			-ldflags="$(LDFLAGS) -H windowsgui" \
			-o dist/$(BINARY)-windows-$$arch.exe . || exit 1; \
	done
	@rm -f rsrc_windows_*.syso

## Linux build. Native only: this one needs GTK 3 and WebKitGTK 4.1.
linux:
	@mkdir -p dist
	@echo "  linux/$(shell go env GOARCH)"
	@go build $(VCS) -tags "$(TAGS)" -trimpath -ldflags="$(LDFLAGS)" \
		-o dist/$(BINARY)-linux-$(shell go env GOARCH) .

## Every icon a release needs, drawn by the app itself.
icons:
	@go run . --gen-icons build/icons
	@mkdir -p build/windows
	@cp build/icons/app-*.png build/windows/
	@rm -rf build/darwin/cabledrop.iconset
	@mkdir -p build/darwin/cabledrop.iconset
	@for s in 16 32 128 256 512; do \
		cp build/icons/app-$$s.png       build/darwin/cabledrop.iconset/icon_$${s}x$${s}.png; \
		cp build/icons/app-$$((s*2)).png build/darwin/cabledrop.iconset/icon_$${s}x$${s}@2x.png; \
	done
	@iconutil -c icns build/darwin/cabledrop.iconset -o build/darwin/AppIcon.icns
	@rm -rf build/darwin/cabledrop.iconset

## Android launcher icons, same artwork at every density.
icons-android:
	@go run . --gen-android-icons android/app/src/main/res

## Android APK: a WebView shell that opens the same page the phone browser
## gets over `adb reverse`. Needs the Android SDK (compileSdk 36 is what the
## project declares) and a JDK 17+. The build runs through the project's own
## gradle wrapper; the JDK comes from Android Studio's bundled JBR when
## present. Output lands in dist/CableDrop.apk (debug-signed).
SDK     ?= $(HOME)/Library/Android/sdk
JBR     = /Applications/Android Studio.app/Contents/jbr/Contents/Home
GRADLE_ENV = SDK_ROOT="$(SDK)"
# test -x, not $(wildcard): the path contains a space and wildcard would
# split it into two patterns, never matching.
ifneq ($(shell test -x "$(JBR)/bin/java" && echo yes),)
  GRADLE_ENV += JAVA_HOME="$(JBR)"
endif

apk: icons-android
	@mkdir -p dist
	@echo "sdk.dir=$(SDK)" > android/local.properties
	cd android && $(GRADLE_ENV) ./gradlew assembleDebug --console=plain
	@cp android/app/build/outputs/apk/debug/app-debug.apk dist/CableDrop.apk
	@echo "  dist/CableDrop.apk"

test:
	go vet ./... && go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

clean:
	rm -rf $(BINARY) $(APPNAME).app cabledrop.app dist build/icons rsrc_windows_*.syso
