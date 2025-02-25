package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/peterbourgon/ff/v4"
	"github.com/saltosystems-internal/x/log"
	"github.com/sorayaormazabalmayo/general-service/internal/server"
)

// Global variable to store the running server instance
var globalServerInstance *server.Server

// NewGeneralServiceCommand creates and returns the root CLI command
func NewGeneralServiceCommand(logger log.Logger) ff.Command {
	fs := ff.NewFlagSet("general-service")

	return ff.Command{
		Name:      "general-service",
		ShortHelp: "This is the root command for the general-service",
		Usage:     "general-service [FLAGS] <SUBCOMMANDS> ...",
		Flags:     fs,
		Exec: func(context.Context, []string) error {
			return flag.ErrHelp
		},
		Subcommands: []*ff.Command{
			newServeCommand(logger),
		},
	}
}

// newServeCommand returns a usable ff.Command for the serve subcommand
func newServeCommand(logger log.Logger) *ff.Command {
	// Configuration structure
	cfg := &server.Config{}

	logger.Info("Config parameters before parsing: ", "httpAddr:", cfg.HTTPAddr, "internal-httpAddr:", cfg.InternatHTTPAddr, "debug:", cfg.Debug)

	fs := ff.NewFlagSet("serve")
	_ = fs.String(0, "config", "", "config file in yaml format")
	fs.StringVar(&cfg.HTTPAddr, 0, "http-addr", "localhost:8000", "HTTP address")
	fs.StringVar(&cfg.InternatHTTPAddr, 0, "internal-http-addr", "localhost:9000", "Internal HTTP address")
	fs.BoolVarDefault(&cfg.Debug, 0, "debug", false, "Enable debug")
	fs.BoolVarDefault(&cfg.AutoUpdate, 0, "auto-update", false, "Enable updater")
	fs.StringVar(&cfg.MetadataURL, 0, "metadata-url", "https://sorayaormazabalmayo.github.io/TUF_Repository_YubiKey_Vault/metadata", "Metadata URL")

	cmd := &ff.Command{
		Name:      "serve",
		ShortHelp: "This SERVE subcommand starts general-service launching an HTTP server",
		Flags:     fs,
		Exec: func(_ context.Context, args []string) error {
			if cfg.Debug {
				if err := logger.SetAllowedLevel(log.AllowDebug()); err != nil {
					return err
				}
			}

			logger.Info("General server started",
				"http-addr", cfg.HTTPAddr,
				"http-internal-addr", cfg.InternatHTTPAddr,
				"debug", cfg.Debug,
			)

			// Start server
			s, err := server.NewServer(cfg, logger)
			if err != nil {
				return err
			}

			// Store the server instance globally
			globalServerInstance = s

			// Handle graceful shutdown
			//go handleShutdown()

			return s.Run()
		},
	}
	return cmd
}

// handleShutdown waits for a termination signal and shuts down the server
func handleShutdown() {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM) // Catch Ctrl+C or SIGTERM

	<-sig // Wait for the shutdown signal

	fmt.Println("\n🛑 Received shutdown signal. Stopping server...")

	if globalServerInstance != nil {
		globalServerInstance.Shutdown()
	}

	fmt.Println("✅ Server shut down successfully.")
	os.Exit(0)
}
