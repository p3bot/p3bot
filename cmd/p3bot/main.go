// Command p3bot installs, updates, uninstalls, lists, and diagnoses the
// p3bot tools.
package main

import (
	"os"

	"github.com/p3bot/p3bot/internal/cli"
)

func main() {
	os.Exit(cli.ExitCode(cli.Execute(os.Args[1:])))
}
