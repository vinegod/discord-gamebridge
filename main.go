package main

import (
	"log/slog"
	"os"

	"github.com/vinegod/discordgamebridge/internal/app"
)

func main() {
	if err := app.Run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}
