//go:build windows && !embedcli

package main

// bundledCLI is empty in builds without -tags embedcli.
var bundledCLI []byte
