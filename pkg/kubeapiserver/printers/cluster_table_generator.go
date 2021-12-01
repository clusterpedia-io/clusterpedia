package printers

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/kubernetes/pkg/printers"

	"github.com/clusterpedia-io/clusterpedia/pkg/utils"
)

type ClusterTableGenerator struct {
	*printers.HumanReadableGenerator
}

func NewClusterTableGenerator() *ClusterTableGenerator {
	return &ClusterTableGenerator{
		printers.NewTableGenerator(),
	}
}

func (generator *ClusterTableGenerator) GenerateTable(obj runtime.Object, options printers.GenerateOptions) (*metav1.Table, error) {
	table, err := generator.HumanReadableGenerator.GenerateTable(obj, options)
	if err != nil {
		return nil, err
	}

	for i, row := range table.Rows {
		table.Rows[i].Cells = append([]interface{}{""}, row.Cells...)
	}

	if _, err := meta.ListAccessor(obj); err == nil {
		if len(table.Rows) != meta.LenList(obj) {
			return table, nil
		}

		listObjs, err := meta.ExtractList(obj)
		if err != nil {
			return table, nil
		}

		for i, o := range listObjs {
			table.Rows[i].Cells[0] = utils.ExtractClusterName(o)
		}
		return table, nil
	}

	table.Rows[0].Cells[0] = utils.ExtractClusterName(obj)
	return table, nil
}

func (generator *ClusterTableGenerator) TableHandler(columnDefinitions []metav1.TableColumnDefinition, printFunc interface{}) error {
	columnDefinitions = append([]metav1.TableColumnDefinition{
		{Name: "Cluster", Type: "string", Format: "", Description: "The Cluster of resource"},
	}, columnDefinitions...)

	return generator.HumanReadableGenerator.TableHandler(columnDefinitions, printFunc)
}

func (generator *ClusterTableGenerator) With(fns ...func(printers.PrintHandler)) *ClusterTableGenerator {
	for _, fn := range fns {
		fn(generator)
	}
	return generator
}
