package metrics

import (
	"net"
	"strconv"

	"github.com/spf13/pflag"
)

type Options struct {
	Host string
	Port int

	TLSConfig           string
	DisableGZIPEncoding bool
}

func NewMetricsServerOptions() *Options {
	return &Options{
		Host:                "::",
		Port:                8081,
		DisableGZIPEncoding: false,
	}
}

func (o *Options) Validate() []error {
	return nil
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Host, "metrics-host", o.Host, "Host to expose clustersynchro manager metrics on.")
	fs.IntVar(&o.Port, "metrics-port", o.Port, "Port to expose clustersynchro manager metrics on.")

	fs.BoolVar(&o.DisableGZIPEncoding, "metrics-disable-gzip-encoding", o.DisableGZIPEncoding, "Gzip responses when requested by clients via 'Accept-Encoding: gzip' header.")
	fs.StringVar(&o.TLSConfig, "metrics-tls-config", o.TLSConfig, "Path to the TLS configuration file of metrics")
}

func (o *Options) Config() (config Config) {
	return Config{
		Endpoint:            net.JoinHostPort(o.Host, strconv.Itoa(o.Port)),
		TLSConfig:           o.TLSConfig,
		DisableGZIPEncoding: o.DisableGZIPEncoding,
	}
}
