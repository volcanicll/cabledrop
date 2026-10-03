// genicons draws the app's artwork — the launcher icon at every density, the
// macOS iconset, the Windows bitmaps — into a directory.
//
// This lives outside the main package on purpose. `go run .` compiles the
// whole binary, and the entry point imports internal/ui, which is Wails and
// therefore cgo against the platform's webview toolkit. On a Linux build
// machine with no GTK that fails before the icons are ever drawn, which is
// exactly what happened to the Android CI job: it installs a JDK and the
// Android SDK, not a desktop GUI stack, and it only wanted some PNGs.
//
// Keeping the rasteriser behind its own command means icon generation needs
// nothing but the Go toolchain, on any platform.
//
//	genicons --android <res-dir>   launcher icons at every density
//	genicons <dir>                 the app iconset and Windows bitmaps
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/volcanicll/cabledrop/internal/icon"
)

func main() {
	args := os.Args[1:]

	android := false
	if len(args) > 0 && args[0] == "--android" {
		android = true
		args = args[1:]
	}

	if android {
		dir := "android/app/src/main/res"
		if len(args) > 0 {
			dir = args[0]
		}
		if err := icon.GenerateAndroidIcons(dir); err != nil {
			log.Fatal(err)
		}
		return
	}

	dir := "build/icons"
	if len(args) > 0 {
		dir = args[0]
	}
	if err := icon.GenerateIcons(dir); err != nil {
		log.Fatal(err)
	}
	fmt.Println(dir)
}
