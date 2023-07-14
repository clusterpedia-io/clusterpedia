package kubestatemetrics

import (
	"fmt"
	_ "unsafe"

	"k8s.io/apimachinery/pkg/runtime/schema"
	_ "k8s.io/kube-state-metrics/v2/pkg/builder"
	generator "k8s.io/kube-state-metrics/v2/pkg/metric_generator"
)

var generators = map[schema.GroupVersionResource]func(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator{
	{Version: "v1", Resource: "pods"}:       podMetricFamilies,
	{Version: "v1", Resource: "secrets"}:    secretMetricFamilies,
	{Version: "v1", Resource: "nodes"}:      nodeMetricFamilies,
	{Version: "v1", Resource: "namespaces"}: namespaceMetricFamilies,
	{Version: "v1", Resource: "services"}:   serviceMetricFamilies,

	{Group: "apps", Version: "v1", Resource: "deployments"}:  deploymentMetricFamilies,
	{Group: "apps", Version: "v1", Resource: "daemonsets"}:   daemonSetMetricFamilies,
	{Group: "apps", Version: "v1", Resource: "statefulsets"}: statefulSetMetricFamilies,
	{Group: "apps", Version: "v1", Resource: "replicasets"}:  replicaSetMetricFamilies,

	{Group: "batch", Version: "v1", Resource: "jobs"}:     jobMetricFamilies,
	{Group: "batch", Version: "v1", Resource: "cronjobs"}: cronJobMetricFamilies,

	{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}:      ingressMetricFamilies,
	{Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}: ingressClassMetricFamilies,
}

var rToGVR = make(map[string]schema.GroupVersionResource)

func init() {
	for gvr := range generators {
		if _, ok := rToGVR[gvr.Resource]; ok {
			panic(fmt.Sprintf("%s has been registered", gvr.String()))
		}
		rToGVR[gvr.Resource] = gvr
	}
}

//go:linkname podMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.podMetricFamilies
func podMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname secretMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.secretMetricFamilies
func secretMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname nodeMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.nodeMetricFamilies
func nodeMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname namespaceMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.namespaceMetricFamilies
func namespaceMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname serviceMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.serviceMetricFamilies
func serviceMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname deploymentMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.deploymentMetricFamilies
func deploymentMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname statefulSetMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.statefulSetMetricFamilies
func statefulSetMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname daemonSetMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.daemonSetMetricFamilies
func daemonSetMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname replicaSetMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.replicaSetMetricFamilies
func replicaSetMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname jobMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.jobMetricFamilies
func jobMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname cronJobMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.cronJobMetricFamilies
func cronJobMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname ingressMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.ingressMetricFamilies
func ingressMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname ingressClassMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.ingressClassMetricFamilies
func ingressClassMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator
