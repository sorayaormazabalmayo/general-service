package server

// Config holds necessary server configuration parameters
type Config struct {
	HTTPAddr         string
	InternatHTTPAddr string
	Debug            bool
}

// Valid checks if required values are present.
func (c *Config) Valid() bool {
	return true
}
