package main

import (
	"fmt"
	"os"

	"github.com/lyracorp/xmanager/internal/activity"
	"github.com/lyracorp/xmanager/internal/ai"
	"github.com/lyracorp/xmanager/internal/backup"
	"github.com/lyracorp/xmanager/internal/config"
	"github.com/lyracorp/xmanager/internal/mcp"
	"github.com/lyracorp/xmanager/internal/notify"
	"github.com/lyracorp/xmanager/internal/ops"
	"github.com/lyracorp/xmanager/internal/poller"
	"github.com/lyracorp/xmanager/internal/ssh"
	"github.com/lyracorp/xmanager/internal/storage"
	"github.com/lyracorp/xmanager/internal/tui"
	"github.com/lyracorp/xmanager/internal/web"
	"github.com/lyracorp/xmanager/internal/workflow"
	"github.com/spf13/cobra"
	"gorm.io/gorm"
)

var rootCmd = &cobra.Command{
	Use:     "xmanager",
	Aliases: []string{"vpsm"},
	Short:   "AI-powered TUI + optional web panel for VPS orchestration",
	Long: `XManager — Manage any server like a senior DevOps engineer.

Connect via SSH, deploy projects, monitor fleet metrics, run scripts,
and manage optional self-hosted services. Zero server-side agents.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("XManager %s\n", config.Version)
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
		_, _ = fmt.Scanln(&answer)
		if answer != "y" && answer != "Y" {
			fmt.Println("Aborted.")
			return nil
		}
		return config.Reset()
	},
}

var webCmd = &cobra.Command{
	Use:   "web",
	Short: "Start the optional HTMX web panel",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		cfg.Web.Enabled = true
		db, err := storage.Open(cfg.DataDir)
		if err != nil {
			return err
		}
		applyLogRetention(cfg, db)
		pool := ssh.NewPool()
		cat := ops.New(db, pool)
		cat.Completer = ai.Completer(cfg)
		wf := workflow.New(db, cat)
		wfSched := workflow.NewScheduler(wf)
		wfSched.Start()
		defer wfSched.Stop()
		notifier := workflow.WrapNotifier(buildNotifier(cfg), wf)
		p := poller.New(db, pool, cfg.Poller, notifier)
		p.Start()
		defer p.Stop()
		sched := backup.NewScheduler(db)
		schedStop := make(chan struct{})
		go sched.StartScheduler(pool, schedStop)
		defer close(schedStop)
		return web.Run(web.Options{Config: cfg, DB: db, Pool: pool, Poller: p, Catalog: cat, Workflows: wf})
	},
}

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Start MCP server for AI agents (stdio)",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		db, err := storage.Open(cfg.DataDir)
		if err != nil {
			return err
		}
		applyLogRetention(cfg, db)
		pool := ssh.NewPool()
		cat := ops.New(db, pool)
		cat.Completer = ai.Completer(cfg)
		return mcp.RunStdio(mcp.Options{Config: cfg, DB: db, Pool: pool, Catalog: cat})
	},
}

func buildNotifier(cfg *config.Config) notify.Notifier {
	var ns []notify.Notifier
	if cfg.Telegram.Enabled && cfg.Telegram.BotToken != "" {
		ns = append(ns, notify.NewTelegram(cfg.Telegram.BotToken, cfg.Telegram.ChatID))
	}
	if cfg.Email.Enabled && cfg.Email.SMTPHost != "" {
		ns = append(ns, notify.NewEmail(
			cfg.Email.SMTPHost, cfg.Email.SMTPPort,
			cfg.Email.Username, cfg.Email.Password, cfg.Email.From,
			[]string{cfg.Email.From},
		))
	}
	if len(ns) == 0 {
		return nil
	}
	return notify.NewMulti(ns...)
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
	applyLogRetention(cfg, db)

	pool := ssh.NewPool()
	cat := ops.New(db, pool)
	cat.Completer = ai.Completer(cfg)
	wf := workflow.New(db, cat)
	wfSched := workflow.NewScheduler(wf)
	wfSched.Start()
	defer wfSched.Stop()
	notifier := workflow.WrapNotifier(buildNotifier(cfg), wf)
	p := poller.New(db, pool, cfg.Poller, notifier)
	p.Start()
	defer p.Stop()

	sched := backup.NewScheduler(db)
	schedStop := make(chan struct{})
	go sched.StartScheduler(pool, schedStop)
	defer close(schedStop)

	opts := tui.AppOptions{
		Config:    cfg,
		DB:        db,
		Pool:      pool,
		Poller:    p,
		Catalog:   cat,
		Workflows: wf,
	}
	if len(initialServer) > 0 {
		opts.InitialTarget = initialServer[0]
	}

	// Optionally start web panel alongside TUI
	if cfg.Web.Enabled {
		go func() {
			_ = web.Run(web.Options{Config: cfg, DB: db, Pool: pool, Poller: p, Catalog: cat, Workflows: wf})
		}()
	}

	return tui.Run(opts)
}

func applyLogRetention(cfg *config.Config, db *gorm.DB) {
	if cfg == nil {
		return
	}
	activity.SetRetentionDays(cfg.Log.RetentionDays)
	activity.Trim(db)
}

func main() {
	rootCmd.AddCommand(versionCmd, connectCmd, setupCmd, resetCmd, webCmd, mcpCmd)
	rootCmd.CompletionOptions.HiddenDefaultCmd = true

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
