package options

import (
	"fmt"

	"github.com/spf13/pflag"
)

type ReadinessProbeOption struct {
	// Number of seconds after which the probe times out.
	// Defaults to 5 second. Minimum value is 1.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty" protobuf:"varint,3,opt,name=timeoutSeconds"`
	// How often (in seconds) to perform the probe.
	// Default to 5 seconds. Minimum value is 5.
	// +optional
	PeriodSeconds int32 `json:"periodSeconds,omitempty" protobuf:"varint,4,opt,name=periodSeconds"`
	// Minimum consecutive failures for the probe to be considered failed after having succeeded.
	// Defaults to 3. Minimum value is 1.
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty" protobuf:"varint,6,opt,name=failureThreshold"`
}

func NewReadinessProbeOption() *ReadinessProbeOption {
	return &ReadinessProbeOption{
		TimeoutSeconds:   5,
		PeriodSeconds:    5,
		FailureThreshold: 3,
	}
}

func (o *ReadinessProbeOption) Validate() []error {
	if o == nil {
		return nil
	}

	var errs []error

	if o.TimeoutSeconds <= 0 {
		errs = append(errs, fmt.Errorf("readiness-timeout-seconds must be greater than 0"))
	}

	if o.PeriodSeconds < 3 {
		errs = append(errs, fmt.Errorf("readiness-period-seconds must be greater than or equal to 3"))
	}

	if o.FailureThreshold <= 0 {
		errs = append(errs, fmt.Errorf("readiness-failure-threshold must be greater than 0"))
	}

	return errs
}

func (o *ReadinessProbeOption) AddFlags(fs *pflag.FlagSet) {
	fs.Int32Var(&o.TimeoutSeconds, "readiness-timeout-seconds", o.TimeoutSeconds, "Number of seconds after which the probe times out for cluster health monitoring.")
	fs.Int32Var(&o.PeriodSeconds, "readiness-period-seconds", o.PeriodSeconds, "How often (in seconds) to perform the probe for cluster health monitoring.")
	fs.Int32Var(&o.FailureThreshold, "readiness-failure-threshold", o.FailureThreshold, "Minimum consecutive failures for the probe to be considered failed after having succeeded.")
}
