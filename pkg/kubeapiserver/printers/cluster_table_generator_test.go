package printers

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/kubernetes/pkg/printers"
)

type TestObject struct {
	TestData string
}

func (obj *TestObject) GetObjectKind() schema.ObjectKind { return schema.EmptyObjectKind }
func (obj *TestObject) DeepCopyObject() runtime.Object {
	if obj == nil {
		return nil
	}
	clone := *obj
	return &clone
}

func ErrorPrintHandler(obj *TestObject, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	return nil, fmt.Errorf("this is a error about the print handler")
}

func TestPrintHandlerError(t *testing.T) {
	columns := []metav1.TableColumnDefinition{
		{
			Name:        "Custom",
			Type:        "string",
			Description: "The custom resource",
		},
	}
	generator := NewClusterTableGenerator()
	err := generator.TableHandler(columns, ErrorPrintHandler)
	if err != nil {
		t.Fatalf("error when adding print handler with given set of columns: %#v", err)
	}

	generateOptions := printers.GenerateOptions{}
	object := TestObject{
		TestData: "Data",
	}
	_, err = generator.GenerateTable(&object, generateOptions)
	if err == nil || err.Error() != "this is a error about the print handler" {
		t.Errorf("can not get the expected error: %#v", err)
	}
}

func PrintCustomType(obj *TestObject, options printers.GenerateOptions) ([]metav1.TableRow, error) {
	return []metav1.TableRow{
		{
			Cells: []interface{}{
				obj.TestData,
			},
		},
	}, nil
}

func TestCustomTypePrinting(t *testing.T) {
	columns := []metav1.TableColumnDefinition{
		{
			Name:        "Custom",
			Type:        "string",
			Description: "The Customization of resource",
		},
	}
	generator := NewClusterTableGenerator()
	err := generator.TableHandler(columns, PrintCustomType)
	if err != nil {
		t.Fatalf("error when adding print handler with given set of columns: %#v", err)
	}

	generateOptions := printers.GenerateOptions{}
	object := TestObject{
		TestData: "Data",
	}
	customTable, err := generator.GenerateTable(&object, generateOptions)
	if err != nil {
		t.Fatalf("error occurred when generating the custom type table: %#v", err)
	}

	expectedTable := &metav1.Table{
		ColumnDefinitions: []metav1.TableColumnDefinition{
			{
				Name:        "Cluster",
				Type:        "string",
				Description: "The Cluster of resource",
			},
			{
				Name:        "Custom",
				Type:        "string",
				Description: "The Customization of resource",
			},
		},
		Rows: []metav1.TableRow{
			{
				Cells: []interface{}{
					"",
					"Data",
				},
			},
		},
	}
	if !reflect.DeepEqual(expectedTable, customTable) {
		t.Errorf("error generating table from custom type. expected (%#v), got (%#v)", expectedTable, customTable)
	}
}
