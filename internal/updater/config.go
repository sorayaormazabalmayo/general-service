package updater

import (
	stdlog "log"
	"os"

	"github.com/go-logr/stdr"
	"github.com/sorayaormazabalmayo/general-service/internal/server"
	"github.com/theupdateframework/go-tuf/v2/metadata"
)

// Config holds necessary server configuration parameters
type Updater struct {
	Logger      metadata.Logger
	metadataURL string
}

// Valid checks if required values are present.
func NewUpdater(cfg *server.Config) *Updater {

	// Set logger to stdout with info level
	metadata.SetLogger(stdr.New(stdlog.New(os.Stdout, "client_example", stdlog.LstdFlags)))
	stdr.SetVerbosity(verbosity)

	log := metadata.GetLogger()

	return &Updater{
		Logger:      log,
		metadataURL: cfg.MetadataURL,
	}
}
