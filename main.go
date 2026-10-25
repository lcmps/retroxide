package main

import (
	"log/slog"
	"os"

	"github.com/lcmps/retroxide/internal/bootstrap"
)

func main() {
	if err := bootstrap.RunFromEnvironment(); err != nil {
		slog.Error("retroxide stopped", "error", err)
		os.Exit(1)
	}
}
