package main

import (
	"os"

	"github.com/Tr0sT/multica-declarative/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:], os.Stdout, os.Stderr))
}
