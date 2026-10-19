//go:build windows && embedcli

package main

import (
	_ "embed"
)

// bundledCLI is the gabitotl.exe built by `make manager-windows` and embedded
// so the single-file setup installs the CLI too.
//
//go:embed bundled/gabitotl.exe
var bundledCLI []byte
