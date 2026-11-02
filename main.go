package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/lcmps/retroxide/bootstrap"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-version") {
		fmt.Println(version)
		return
	}
	if err := bootstrap.RunFromEnvironment(); err != nil {
		slog.Error("retroxide stopped", "error", err)
		os.Exit(1)
	}
}
