// +gocover:ignore:file entry point and only delegates execution
package main

import (
	_ "embed"
	"os"

	"github.com/br-lemes/fileclosure/cmd"
)

//go:embed .version
var version string

func main() {
	err := cmd.Execute(version)
	if err != nil {
		os.Exit(1)
	}
}
