// Build the Windows installer (Chinese wizard) for Show Me The Story.
//
// Replaces a plain tar/zip step in release.yml. Pure Go, no external tools:
//   - embeds a multi-size app icon + version info into the .exe via a generated
//     Windows resource (.syso) so Explorer shows our icon and the Details tab works;
//   - builds with -H windowsgui so double-clicking does not open a black console;
//   - produces an NSIS installer with a Simplified-Chinese wizard.
//
// Usage (from repo root, GOOS=windows GOARCH=amd64 already set):
//   go run ./tools/buildwin -version v4.0.2 -out dist
//
// The NSIS script itself lives in build/windows/installer.nsi; makensis is
// expected on PATH (CI installs it via choco/apt).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type winres struct {
	GroupIcon map[string]map[string][]string   `json:"RT_GROUP_ICON"`
	Version   map[string]map[string]versionRes `json:"RT_VERSION"`
	Manifest  map[string]map[string]string     `json:"RT_MANIFEST"`
}

type versionRes struct {
	Fixed struct {
		FileVersion    string `json:"file_version"`
		ProductVersion string `json:"product_version"`
	} `json:"fixed"`
	Info map[string]map[string]string `json:"info"`
}

func main() {
	version := flag.String("version", "dev", "version string, e.g. v4.0.2")
	outDir := flag.String("out", "dist", "output directory")
	skipInstaller := flag.Bool("skip-installer", false, "build only the exe, skip NSIS")
	flag.Parse()

	root, err := repoRoot()
	must(err)

	ver := strings.TrimPrefix(*version, "v")
	numeric := toNumericVersion(ver)

	must(os.MkdirAll(filepath.Join(root, *outDir), 0o755))

	// 1) Windows resource: icon + version info + manifest.
	must(writeWinresJSON(root, ver, numeric))
	sysoOut := filepath.Join(root, "rs_showmethestory")
	run(root, "go", "run", "github.com/tc-hib/go-winres@v0.3.3", "make",
		"--in", filepath.Join("build", "windows", "winres.json"),
		"--out", sysoOut, "--arch", "amd64")
	fmt.Println("[buildwin] resource syso generated")

	// 2) GUI binary (no console window).
	exeName := "show-me-the-story.exe"
	exePath := filepath.Join(root, *outDir, exeName)
	run(root, "go", "build",
		"-trimpath",
		"-ldflags", fmt.Sprintf("-s -w -H windowsgui -X main.version=%s", *version),
		"-o", exePath, ".")
	fmt.Println("[buildwin] exe built:", exePath, humanSize(exePath))

	if *skipInstaller {
		return
	}

	// 3) NSIS installer with Simplified-Chinese wizard.
	makensis, err := exec.LookPath("makensis")
	if err != nil {
		fmt.Println("[buildwin] WARNING: makensis not found on PATH — skipping installer")
		fmt.Println("[buildwin] install NSIS 3.x and re-run, or see build/windows/README.md")
		return
	}
	nsi := filepath.Join(root, "build", "windows", "installer.nsi")
	run(filepath.Dir(nsi), makensis, "/V2",
		"/DAPP_VERSION="+ver,
		"/DAPP_VERSION_NUMERIC="+numeric,
		"/DSOURCE_EXE="+exePath,
		"/DOUTPUT_DIR="+filepath.Join(root, *outDir),
		nsi)
	if err := verifyPE(filepath.Join(root, *outDir, exeName)); err != nil {
		fatal(err)
	}
	fmt.Println("[buildwin] done")
}

func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found")
		}
		dir = parent
	}
}

// toNumericVersion turns "4.0.2" into "4.0.2.0" (Windows wants 4 parts).
func toNumericVersion(v string) string {
	parts := strings.Split(strings.TrimSpace(v), ".")
	for len(parts) < 4 {
		parts = append(parts, "0")
	}
	if len(parts) > 4 {
		parts = parts[:4]
	}
	for i, p := range parts {
		// strip any non-digit suffix (e.g. "2-beta" -> "2")
		var b strings.Builder
		for _, r := range p {
			if r >= '0' && r <= '9' {
				b.WriteRune(r)
			} else {
				break
			}
		}
		if b.Len() == 0 {
			b.WriteString("0")
		}
		parts[i] = b.String()
	}
	return strings.Join(parts, ".")
}

func writeWinresJSON(root, ver, numeric string) error {
	// Icon PNGs are generated from docs/show-me-the-story.webp by the CI step
	// "Generate app icon" (Python + Pillow); if they are missing we still want a
	// clear error rather than a silent iconless build.
	iconDir := filepath.Join(root, "build", "windows")
	sizes := []int{256, 128, 64, 48, 32, 24, 16}
	var icons []string
	for _, s := range sizes {
		name := fmt.Sprintf("icon_%d.png", s)
		if _, err := os.Stat(filepath.Join(iconDir, name)); err != nil {
			return fmt.Errorf("missing %s — run tools/make_icon.py first", filepath.Join("build/windows", name))
		}
		icons = append(icons, name)
	}

	r := map[string]any{
		"RT_GROUP_ICON": map[string]any{
			"APP": map[string]any{"0000": icons},
		},
		"RT_VERSION": map[string]any{
			"1": map[string]any{
				"0000": map[string]any{
					"fixed": map[string]string{
						"file_version":    numeric,
						"product_version": numeric,
					},
					"info": map[string]any{
						"0409": map[string]string{
							"CompanyName":      "xianii",
							"FileDescription":  "Show Me The Story - AI novel writing workspace",
							"FileVersion":      ver,
							"InternalName":     "show-me-the-story",
							"LegalCopyright":   "MIT License",
							"OriginalFilename": "show-me-the-story.exe",
							"ProductName":      "Show Me The Story",
							"ProductVersion":   ver,
						},
						"0804": map[string]string{
							"CompanyName":      "xianii",
							"FileDescription":  "AI 小说写手",
							"FileVersion":      ver,
							"InternalName":     "show-me-the-story",
							"LegalCopyright":   "MIT License",
							"OriginalFilename": "show-me-the-story.exe",
							"ProductName":      "AI 小说写手",
							"ProductVersion":   ver,
						},
					},
				},
			},
		},
		"RT_MANIFEST": map[string]any{
			"1": map[string]any{"0000": map[string]string{"file": "app.manifest"}},
		},
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(iconDir, "winres.json"), data, 0o644)
}

func verifyPE(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	head := make([]byte, 0x200)
	if _, err := f.Read(head); err != nil {
		return err
	}
	if head[0] != 'M' || head[1] != 'Z' {
		return fmt.Errorf("%s: not a PE file", path)
	}
	return nil
}

func run(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if runtime.GOOS != "windows" {
		// Cross-building for Windows still works: go build honours GOOS/GOARCH.
	}
	if err := cmd.Run(); err != nil {
		fatal(fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err))
	}
}

func humanSize(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "?"
	}
	return fmt.Sprintf("%.2f MiB", float64(st.Size())/(1024*1024))
}

func must(err error) {
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "[buildwin] error:", err)
	os.Exit(1)
}
