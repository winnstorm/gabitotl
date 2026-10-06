// Command gabitotl is the GabitoTL installer and project tooling entry point.
package main

import (
	"os"

	"github.com/winnstorm/gabitotl/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], cli.DefaultEnv()))
}
