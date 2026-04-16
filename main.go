package main

import (
	"fmt"
	"os"

	"github.com/thepixelabs/amensiai/cmd"
	_ "github.com/thepixelabs/amensiai/internal/provider/all"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
