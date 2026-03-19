package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/vinegod/discordgamebridge/internal/app"
	"github.com/vinegod/discordgamebridge/internal/version"
)

func main() {
	// Define flags
	configPath := flag.String("config", "config.yaml", "Path to the configuration file")
	showVersion := flag.Bool("version", false, "Print application version and exit")
	validateOnly := flag.Bool("validate", false, "Validate the config file and exit")
	debugMode := flag.Bool("debug", false, "Force debug logging (overrides config)")

	flag.Parse()

	// 1. Handle Version
	if *showVersion {
		fmt.Printf("Discord Gamebridge v%s\n", version.Version)
		os.Exit(0)
	}

	// 2. Handle Initialization & Validation
	application := app.New(*configPath)

	if *validateOnly {
		application.ForceDebug = true
		if _, err := application.LoadConfiguration(); err != nil {
			fmt.Printf("Configuration is invalid: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Configuration is valid.")
		os.Exit(0)
	}

	if *debugMode {
		application.ForceDebug = true
	}

	if err := application.Run(); err != nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}
