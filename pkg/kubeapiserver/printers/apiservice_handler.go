package printers

import (
	"fmt"

	metatable "k8s.io/apimachinery/pkg/api/meta/table"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kube-aggregator/pkg/apis/apiregistration"
	"k8s.io/kubernetes/pkg/printers"
)

func AddAPIServiceHandler(h printers.PrintHandler) {
	columnDefinitions := []metav1.TableColumnDefinition{
		{Name: "Name", Type: "string", Format: "name", Description: swaggerMetadataDescriptions["name"]},
		{Name: "Service", Type: "string", Description: "The reference to the service that hosts this API endpoint."},
		{Name: "Available", Type: "string", Description: "Whether this service is available."},
		{Name: "Age", Type: "string", Description: swaggerMetadataDescriptions["creationTimestamp"]},
	}

	_ = h.TableHandler(columnDefinitions, func(list *apiregistration.APIServiceList, options printers.GenerateOptions) ([]metav1.TableRow, error) {
		return metatable.MetaToTableRow(list, apiServiceHandler)
	})
	_ = h.TableHandler(columnDefinitions, func(apiservice *apiregistration.APIService, options printers.GenerateOptions) ([]metav1.TableRow, error) {
		return metatable.MetaToTableRow(apiservice, apiServiceHandler)
	})
}

// k8s.io/kube-aggregator/pkg/registry/apiservice/etcd.(*REST).ConvertToTable
func apiServiceHandler(obj runtime.Object, m metav1.Object, name, age string) ([]interface{}, error) {
	svc := obj.(*apiregistration.APIService)
	service := "Local"
	if svc.Spec.Service != nil {
		service = fmt.Sprintf("%s/%s", svc.Spec.Service.Namespace, svc.Spec.Service.Name)
	}
	status := string(apiregistration.ConditionUnknown)
	if condition := getCondition(svc.Status.Conditions, "Available"); condition != nil {
		switch {
		case condition.Status == apiregistration.ConditionTrue:
			status = string(condition.Status)
		case len(condition.Reason) > 0:
			status = fmt.Sprintf("%s (%s)", condition.Status, condition.Reason)
		default:
			status = string(condition.Status)
		}
	}
	return []interface{}{name, service, status, age}, nil
}

func getCondition(conditions []apiregistration.APIServiceCondition, conditionType apiregistration.APIServiceConditionType) *apiregistration.APIServiceCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}
