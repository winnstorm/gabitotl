// Command gen writes the manager's icon assets: appicon.png, appicon.ico and
// rsrc_windows_amd64.syso (icon, version info and manifest, linked in by the Go
// toolchain on Windows builds). Run it through `go generate`.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tc-hib/winres"
	"github.com/tc-hib/winres/version"

	"github.com/winnstorm/gabitotl/cmd/gabitotl-manager/internal/icongen"
)

func main() {
	out := flag.String("out", ".", "directory of cmd/gabitotl-manager")
	ver := flag.String("version", "0.1.0.0", "file and product version, four numbers")
	flag.Parse()
	if err := run(*out, *ver); err != nil {
		fmt.Fprintln(os.Stderr, "icongen:", err)
		os.Exit(1)
	}
}

func run(dir, ver string) error {
	fv, err := parseVersion(ver)
	if err != nil {
		return err
	}
	pngBytes, err := icongen.PNG(256)
	if err != nil {
		return err
	}
	icoBytes, err := icongen.ICO(icongen.Sizes)
	if err != nil {
		return err
	}
	icon, err := winres.LoadICO(bytes.NewReader(icoBytes))
	if err != nil {
		return err
	}
	rs := &winres.ResourceSet{}
	if err := rs.SetIcon(winres.Name("APP"), icon); err != nil {
		return err
	}
	vi := version.Info{}
	vi.SetFileVersion(fv)
	vi.SetProductVersion(fv)
	for k, v := range map[string]string{
		version.ProductName:      "GabitoTL",
		version.FileDescription:  "GabitoTL setup and manager",
		version.CompanyName:      "winnstorm",
		version.LegalCopyright:   "MIT License",
		version.OriginalFilename: "gabitotl-manager.exe",
		version.InternalName:     "gabitotl-manager",
	} {
		if err := vi.Set(version.LangDefault, k, v); err != nil {
			return err
		}
	}
	rs.SetVersionInfo(vi)
	rs.SetManifest(winres.AppManifest{
		Description:         "GabitoTL setup and manager",
		ExecutionLevel:      winres.AsInvoker,
		DPIAwareness:        winres.DPIPerMonitorV2,
		UseCommonControlsV6: true,
		Compatibility:       winres.Win10AndAbove,
	})
	var obj bytes.Buffer
	if err := rs.WriteObject(&obj, winres.ArchAMD64); err != nil {
		return err
	}
	for name, data := range map[string][]byte{
		"appicon.png":             pngBytes,
		"appicon.ico":             icoBytes,
		"rsrc_windows_amd64.syso": obj.Bytes(),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func parseVersion(s string) (string, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return "", fmt.Errorf("version %q must have four numbers, like 0.1.0.0", s)
	}
	for _, p := range parts {
		if _, err := strconv.ParseUint(p, 10, 16); err != nil {
			return "", fmt.Errorf("version %q: %w", s, err)
		}
	}
	return s, nil
}
