package options

import (
	"fmt"
	"net"

	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apiserver/pkg/admission/plugin/namespace/lifecycle"
	"k8s.io/apiserver/pkg/features"
	genericapiserver "k8s.io/apiserver/pkg/server"
	genericoptions "k8s.io/apiserver/pkg/server/options"
	"k8s.io/apiserver/pkg/util/feature"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	utilflowcontrol "k8s.io/apiserver/pkg/util/flowcontrol"
	"k8s.io/client-go/kubernetes"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/featuregate"
	"k8s.io/klog/v2"

	"github.com/clusterpedia-io/clusterpedia/pkg/apiserver"
	"github.com/clusterpedia-io/clusterpedia/pkg/storage"
	storageoptions "github.com/clusterpedia-io/clusterpedia/pkg/storage/options"
)

type ClusterPediaServerOptions struct {
	SecureServing  *genericoptions.SecureServingOptionsWithLoopback
	Authentication *genericoptions.DelegatingAuthenticationOptions
	Authorization  *genericoptions.DelegatingAuthorizationOptions
	Audit          *genericoptions.AuditOptions
	Features       *genericoptions.FeatureOptions
	CoreAPI        *genericoptions.CoreAPIOptions
	FeatureGate    featuregate.FeatureGate
	Admission      *genericoptions.AdmissionOptions
	//	Traces         *genericoptions.TracingOptions

	Storage *storageoptions.StorageOptions
}

func NewServerOptions() *ClusterPediaServerOptions {
	sso := genericoptions.NewSecureServingOptions()

	// We are composing recommended options for an aggregated api-server,
	// whose client is typically a proxy multiplexing many operations ---
	// notably including long-running ones --- into one HTTP/2 connection
	// into this server.  So allow many concurrent operations.
	sso.HTTP2MaxStreamsPerConnection = 1000

	return &ClusterPediaServerOptions{
		SecureServing:  sso.WithLoopback(),
		Authentication: genericoptions.NewDelegatingAuthenticationOptions(),
		Authorization:  genericoptions.NewDelegatingAuthorizationOptions(),
		Audit:          genericoptions.NewAuditOptions(),
		Features:       genericoptions.NewFeatureOptions(),
		CoreAPI:        genericoptions.NewCoreAPIOptions(),
		FeatureGate:    feature.DefaultFeatureGate,
		Admission:      genericoptions.NewAdmissionOptions(),
		//	Traces:         genericoptions.NewTracingOptions(),

		Storage: storageoptions.NewStorageOptions(),
	}
}

func (o *ClusterPediaServerOptions) Validate(args []string) error {
	errors := []error{}
	errors = append(errors, o.validateGenericOptions()...)
	errors = append(errors, o.Storage.Validate()...)

	return utilerrors.NewAggregate(errors)
}

func (o *ClusterPediaServerOptions) Complete() error {
	return nil
}

func (o *ClusterPediaServerOptions) Config() (*apiserver.Config, error) {
	storage, err := storage.NewStorageFactory(o.Storage.Name, o.Storage.ConfigPath)
	if err != nil {
		return nil, err
	}

	if err := o.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error create self-signed certificates: %v", err)
	}

	// remove NamespaceLifecycle admission plugin explicitly
	// current admission plugins:  mutatingwebhook, validatingwebhook
	o.Admission.DisablePlugins = append(o.Admission.DisablePlugins, lifecycle.PluginName)

	genericConfig := genericapiserver.NewRecommendedConfig(apiserver.Codecs)
	// genericConfig.OpenAPIConfig = genericapiserver.DefaultOpenAPIConfig(openapi.GetOpenAPIDefinitions, openapi.NewDefinitionNamer(apiserver.Scheme))
	// genericConfig.OpenAPIConfig.Info.Title = openAPITitle
	// genericConfig.OpenAPIConfig.Info.Version= openAPIVersion

	if err := o.genericOptionsApplyTo(genericConfig); err != nil {
		return nil, err
	}

	return &apiserver.Config{
		GenericConfig:  genericConfig,
		StorageFactory: storage,
	}, nil
}

func (o *ClusterPediaServerOptions) genericOptionsApplyTo(config *genericapiserver.RecommendedConfig) error {
	if err := o.SecureServing.ApplyTo(&config.SecureServing, &config.LoopbackClientConfig); err != nil {
		return err
	}
	if err := o.Authentication.ApplyTo(&config.Authentication, config.SecureServing, config.OpenAPIConfig); err != nil {
		return err
	}
	if err := o.Authorization.ApplyTo(&config.Authorization); err != nil {
		return err
	}
	if err := o.Audit.ApplyTo(&config.Config); err != nil {
		return err
	}
	if err := o.Features.ApplyTo(&config.Config); err != nil {
		return err
	}
	if err := o.CoreAPI.ApplyTo(config); err != nil {
		return err
	}
	if err := o.Admission.ApplyTo(&config.Config, config.SharedInformerFactory, config.ClientConfig, o.FeatureGate); err != nil {
		return err
	}

	if utilfeature.DefaultFeatureGate.Enabled(features.APIPriorityAndFairness) {
		if config.ClientConfig != nil {
			config.FlowControl = utilflowcontrol.New(
				config.SharedInformerFactory,
				kubernetes.NewForConfigOrDie(config.ClientConfig).FlowcontrolV1beta1(),
				config.MaxRequestsInFlight+config.MaxMutatingRequestsInFlight,
				config.RequestTimeout/4,
			)
		} else {
			klog.Warningf("Neither kubeconfig is provided nor service-account is mounted, so APIPriorityAndFairness will be disabled")
		}
	}
	return nil
}

func (o *ClusterPediaServerOptions) Flags() cliflag.NamedFlagSets {
	var fss cliflag.NamedFlagSets

	o.CoreAPI.AddFlags(fss.FlagSet("global"))
	o.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	o.Authentication.AddFlags(fss.FlagSet("authentication"))
	o.Authorization.AddFlags(fss.FlagSet("authorization"))
	o.Audit.AddFlags(fss.FlagSet("auditing"))
	o.Features.AddFlags(fss.FlagSet("features"))

	// o.Admission.AddFlags(fss.FlagSet("admission"))
	// o.Traces.AddFlags(fss.FlagSet("traces"))

	o.Storage.AddFlags(fss.FlagSet("storage"))
	return fss
}

func (o *ClusterPediaServerOptions) validateGenericOptions() []error {
	errors := []error{}
	errors = append(errors, o.CoreAPI.Validate()...)
	errors = append(errors, o.SecureServing.Validate()...)
	errors = append(errors, o.Authentication.Validate()...)
	errors = append(errors, o.Authorization.Validate()...)
	errors = append(errors, o.Audit.Validate()...)
	errors = append(errors, o.Features.Validate()...)
	return errors
}
