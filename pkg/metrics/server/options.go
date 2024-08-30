package server

import (
	"net"
	"strconv"

	"github.com/spf13/pflag"
	"k8s.io/component-base/metrics"
)

type Options struct {
	Host string
	Port int

	TLSConfig           string
	DisableGZIPEncoding bool

	Metrics *metrics.Options
}

func NewOptions() *Options {
	return &Options{
		Host:                "::",
		Port:                8081,
		DisableGZIPEncoding: false,

		Metrics: metrics.NewOptions(),
	}
}

func (o *Options) Validate() []error {
	return o.Metrics.Validate()
}

func (o *Options) AddFlags(fs *pflag.FlagSet) {
	fs.StringVar(&o.Host, "metrics-host", o.Host, "Host to expose clustersynchro manager metrics on.")
	fs.IntVar(&o.Port, "metrics-port", o.Port, "Port to expose clustersynchro manager metrics on.")

	fs.BoolVar(&o.DisableGZIPEncoding, "metrics-disable-gzip-encoding", o.DisableGZIPEncoding, "Gzip responses when requested by clients via 'Accept-Encoding: gzip' header.")
	fs.StringVar(&o.TLSConfig, "metrics-tls-config", o.TLSConfig, "Path to the TLS configuration file of metrics")
	o.Metrics.AddFlags(fs)
}

func (o *Options) Config() (config Config) {
	o.Metrics.Apply()

	return Config{
		Endpoint:            net.JoinHostPort(o.Host, strconv.Itoa(o.Port)),
		TLSConfig:           o.TLSConfig,
		DisableGZIPEncoding: o.DisableGZIPEncoding,
	}
}
