package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/andrewmcodes/shortcut-source-saver/internal/shortcuts"
)

var version = "dev"

func main() {
	if err := newRootCommand().Execute(); err != nil {
		if errors.Is(err, shortcuts.ErrNoAccess) {
			fmt.Fprintf(os.Stderr, "\nerror: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
		os.Exit(1)
	}
}
