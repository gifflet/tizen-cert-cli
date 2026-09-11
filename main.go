// tizen-cert is an interactive CLI that generates a complete, valid Samsung
// Tizen TV certificate profile (author + distributor) for N TVs, detected
// via sdb or entered manually, and registers it in profiles.xml.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gifflet/tizen-cert-cli/internal/tui"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("tizen-cert " + version)
		return
	}

	program := tea.NewProgram(tui.New())
	if _, err := program.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
