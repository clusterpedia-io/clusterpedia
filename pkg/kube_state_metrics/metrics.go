package kubestatemetrics

import (
	_ "unsafe"

	"k8s.io/apimachinery/pkg/runtime/schema"
	_ "k8s.io/kube-state-metrics/v2/pkg/builder"
	generator "k8s.io/kube-state-metrics/v2/pkg/metric_generator"
)

var generators = map[schema.GroupVersionResource][]generator.FamilyGenerator{
	schema.GroupVersionResource{Version: "v1", Resource: "pods"}:                        podMetricFamilies(nil, nil),
	schema.GroupVersionResource{Version: "v1", Resource: "nodes"}:                       nodeMetricFamilies(nil, nil),
	schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}:                  namespaceMetricFamilies(nil, nil),
	schema.GroupVersionResource{Version: "v1", Resource: "services"}:                    serviceMetricFamilies(nil, nil),
	schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}:  deploymentMetricFamilies(nil, nil),
	schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}: statefulSetMetricFamilies(nil, nil),
	schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}:  replicaSetMetricFamilies(nil, nil),
	schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}:        jobMetricFamilies(nil, nil),
	schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}:    cronJobMetricFamilies(nil, nil),
}

//go:linkname podMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.podMetricFamilies
func podMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname nodeMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.nodeMetricFamilies
func nodeMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname namespaceMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.namespaceMetricFamilies
func namespaceMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname deploymentMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.deploymentMetricFamilies
func deploymentMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname statefulSetMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.statefulSetMetricFamilies
func statefulSetMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname serviceMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.serviceMetricFamilies
func serviceMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname replicaSetMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.replicaSetMetricFamilies
func replicaSetMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname jobMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.jobMetricFamilies
func jobMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator

//go:linkname cronJobMetricFamilies k8s.io/kube-state-metrics/v2/internal/store.cronJobMetricFamilies
func cronJobMetricFamilies(allowAnnotationsList, allowLabelsList []string) []generator.FamilyGenerator
