// Package main is the single-binary entrypoint for TuTu.
// TuTu is the simplest way to run AI locally — one binary, zero dependencies.
package main

import "github.com/tutu-network/tutu/internal/cli"

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	cli.Execute(version)
}
