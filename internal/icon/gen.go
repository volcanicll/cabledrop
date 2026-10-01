package icon

import (
	"fmt"
	"os"
	"path/filepath"
)

// generateIcons writes every icon a release needs, from the same drawing code
// the tray uses. Invoked as `cabledrop --gen-icons <dir>` from the Makefile, so
// the artwork can never drift from what the app actually renders.
func GenerateIcons(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	write := func(name string, data []byte) error {
		if len(data) == 0 {
			return fmt.Errorf("生成 %s 失败", name)
		}
		return os.WriteFile(filepath.Join(dir, name), data, 0o644)
	}

	if err := write("tray-on.png", TrayIcon(true)); err != nil {
		return err
	}
	if err := write("tray-off.png", TrayIcon(false)); err != nil {
		return err
	}

	// The sizes Windows and the macOS .iconset want.
	for _, size := range []int{16, 32, 48, 64, 128, 256, 512, 1024} {
		if err := write(fmt.Sprintf("app-%d.png", size), AppIconPNG(size)); err != nil {
			return err
		}
	}

	fmt.Printf("图标已写入 %s\n", dir)
	return nil
}

// generateAndroidIcons writes the launcher icon at every density the Android
// resource system expects, from the same drawing code as everything else.
func GenerateAndroidIcons(dir string) error {
	for density, size := range map[string]int{
		"mdpi":    48,
		"hdpi":    72,
		"xhdpi":   96,
		"xxhdpi":  144,
		"xxxhdpi": 192,
	} {
		out := filepath.Join(dir, "mipmap-"+density)
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		name := filepath.Join(out, "ic_launcher.png")
		if err := os.WriteFile(name, AppIconPNG(size), 0o644); err != nil {
			return err
		}
		fmt.Printf("图标已写入 %s\n", name)
	}
	return nil
}
