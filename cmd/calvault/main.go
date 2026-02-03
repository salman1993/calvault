package main

import (
	"os"

	"github.com/salman1993/calvault/cmd/calvault/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
