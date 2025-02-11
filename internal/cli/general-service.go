package cli

import (
	"context"
	"flag"

	"github.com/peterbourgon/ff/v4"
	"github.com/saltosystems-internal/x/log"
	"github.com/sorayaormazabalmayo/general-service/internal/server"
)

// NewGneralServiceCommand creates and returns the root cli command
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
			// list of available subcommands to the root general-service command
			newServeCommand(logger),
		},
	}

}

// newServeCommand returns a usable ff.Command for the serve subcommand
func newServeCommand(logger log.Logger) *ff.Command {

	// This config structure is where the variables are allocated after parsing
	cfg := &server.Config{}

	logger.Info("Config parameters before parsing: ", "httpAddr:", cfg.HTTPAddr, "internal-httpAddr:", cfg.InternatHTTPAddr, "debug:", cfg.Debug)

	fs := ff.NewFlagSet("serve")
	_ = fs.String(0, "config", "", "config file in yaml format")
	// This stores what has been parsed in config
	fs.StringVar(&cfg.HTTPAddr, 0, "http-addr", "localhost:8000", "HTTP address")
	fs.StringVar(&cfg.InternatHTTPAddr, 0, "internal-http-addr", "localhost:9000", "Internal HTTP address")
	fs.BoolVarDefault(&cfg.Debug, 0, "debug", false, "Enable debug")

	cmd := &ff.Command{
		Name:      "serve",
		ShortHelp: "This SERVE subcommand starts general-service launching a http server",
		Flags:     fs,
		Exec: func(_ context.Context, args []string) error { // defining exec inline allows it to access the flags above
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

			s, err := server.NewServer(cfg, logger)
			if err != nil {
				return err
			}

			return s.Run()
		},
	}
	return cmd
}
