package main

import (
	"fmt"
	"os"

	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:     "xmanager",
	Aliases: []string{"vpsm"},
	Short:   "AI-powered terminal UI for VPS orchestration",
	Long: `XManager TUI — Manage any server like a senior DevOps engineer, from your terminal.

Connect to any SSH server, AI identifies everything running, and you manage
it all from a beautiful terminal UI. Zero server-side footprint.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("XManager TUI %s\n", config.Version)
		fmt.Printf("  Commit:  %s\n", config.Commit)
		fmt.Printf("  Built:   %s\n", config.BuildTime)
	},
}

var connectCmd = &cobra.Command{
	Use:   "connect [server]",
	Short: "Connect to a server directly",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI(args[0])
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Run the first-time setup wizard",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI("__setup__")
	},
}

var resetCmd = &cobra.Command{
	Use:   "reset",
	Short: "Reset configuration (with confirmation)",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Print("Are you sure you want to reset all XManager configuration? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
		return config.Reset()
	},
}

func runTUI(initialServer ...string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	db, err := storage.Open(cfg.DataDir)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}

	opts := tui.AppOptions{
		Config: cfg,
		DB:     db,
	}
	if len(initialServer) > 0 {
		opts.InitialTarget = initialServer[0]
	}

	return tui.Run(opts)
}

func main() {
	rootCmd.AddCommand(versionCmd, connectCmd, setupCmd, resetCmd)
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
