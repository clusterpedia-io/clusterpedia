package norm

type Config struct {
	DebugMode bool
	logger    Logger
}

func (config *Config) LoadDefault() {
	if config.logger == nil {
		config.logger = &DefaultLogger{}
	}
}

type Option func(c *Config)

func WithLogger(logger Logger) Option {
	return func(c *Config) {
		c.logger = logger
	}
}
