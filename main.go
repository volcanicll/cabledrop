// CableDrop — move data between a computer and an Android phone over a USB
// cable, with no network involved.
//
// The desktop side is a tray panel; the same embedded pages and the same JSON
// API are also served to the phone through `adb reverse`, so nothing needs to
// be installed on the phone.
//
// This file is only the entry point: flag dispatch, assembly, and the version
// stamp. The packages under internal/ hold everything else.
package main

import (
	"log"
	"os"

	"github.com/volcanicll/cabledrop/internal/app"
	"github.com/volcanicll/cabledrop/internal/icon"
	"github.com/volcanicll/cabledrop/internal/serve"
	"github.com/volcanicll/cabledrop/internal/ui"
)

// version is stamped in at build time:
// -ldflags "-X main.version=$(git describe --tags --always)"
var version = "dev"

func main() {
	args := os.Args[1:]

	// Icon generation runs before any GUI setup, so it works headlessly in a
	// build script.
	if len(args) > 0 {
		switch args[0] {
		case "--gen-icons":
			dir := "build/icons"
			if len(args) > 1 {
				dir = args[1]
			}
			if err := icon.GenerateIcons(dir); err != nil {
				log.Fatal(err)
			}
			return
		case "--gen-android-icons":
			dir := "android/app/src/main/res"
			if len(args) > 1 {
				dir = args[1]
			}
			if err := icon.GenerateAndroidIcons(dir); err != nil {
				log.Fatal(err)
			}
			return
		}
	}

	a := app.New()

	err := ui.Run(ui.Options{
		Name:        "CableDrop",
		Description: "Move files and clipboard text between this computer and an Android phone over USB",
		Version:     version,
		// The panel reaches the same handler and API the phone gets; only the
		// page at "/" differs.
		Handler:       serve.NewHandler(a, false),
		Controller:    a,
		ServeOnLaunch: len(args) > 0 && args[0] == "--serve",
	})
	if err != nil {
		log.Fatal(err)
	}
}
