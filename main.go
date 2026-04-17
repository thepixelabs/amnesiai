package main

import (
	"fmt"
	"os"

	"github.com/thepixelabs/amnesiai/cmd"
	_ "github.com/thepixelabs/amnesiai/internal/provider/all"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
