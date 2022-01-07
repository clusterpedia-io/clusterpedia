package v1alpha1

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	metainternal "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	conversion "k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/clusterpedia-io/clusterpedia/pkg/apis/pedia"
	"github.com/clusterpedia-io/clusterpedia/pkg/utils/fields"
)

func Convert_v1alpha1_ListOptions_To_pedia_ListOptions(in *ListOptions, out *pedia.ListOptions, s conversion.Scope) error {
	fieldSelector := in.FieldSelector
	defer func() {
		in.FieldSelector = fieldSelector
	}()

	// skip convert fieldSelector
	in.FieldSelector = ""
	if err := metainternal.Convert_v1_ListOptions_To_internalversion_ListOptions(&in.ListOptions, &out.ListOptions, s); err != nil {
		return err
	}

	if err := convert_string_To_fields_Selector(&fieldSelector, &out.EnhancedFieldSelector, s); err != nil {
		return err
	}

	if err := convert_String_To_Slice_string(&in.Names, &out.Names, s); err != nil {
		return err
	}
	out.Owner = in.Owner
	if err := convert_String_To_Slice_string(&in.ClusterNames, &out.ClusterNames, s); err != nil {
		return err
	}
	if err := convert_String_To_Slice_string(&in.Namespaces, &out.Namespaces, s); err != nil {
		return err
	}

	var orderbys []string
	if err := convert_String_To_Slice_string(&in.OrderBy, &orderbys, s); err != nil {
		return err
	}
	if err := convert_Slice_string_To_pedia_Slice_orderby(&orderbys, &out.OrderBy, " ", s); err != nil {
		return err
	}

	out.WithContinue = in.WithContinue
	out.WithRemainingCount = in.WithRemainingCount

	if out.LabelSelector != nil {
		var (
			labelRequest      []labels.Requirement
			extraLabelRequest []labels.Requirement
		)

		if requirements, selectable := out.LabelSelector.Requirements(); selectable {
			for _, require := range requirements {
				values := require.Values().UnsortedList()
				switch require.Key() {
				case pedia.SearchLabelOwner:
					if len(out.Owner) == 0 && len(values) != 0 {
						out.Owner = values[0]
					}
				case pedia.SearchLabelNames:
					if len(out.Names) == 0 && len(values) != 0 {
						out.Names = values
					}
				case pedia.SearchLabelClusters:
					if len(out.ClusterNames) == 0 && len(values) != 0 {
						out.ClusterNames = values
					}
				case pedia.SearchLabelNamespaces:
					if len(out.Namespaces) == 0 && len(values) != 0 {
						out.Namespaces = values
					}
				case pedia.SearchLabelOrderBy:
					if len(out.OrderBy) == 0 && len(values) != 0 {
						if err := convert_Slice_string_To_pedia_Slice_orderby(&values, &out.OrderBy, "_", s); err != nil {
							return err
						}
					}
				case pedia.SearchLabelLimit:
					if out.Limit == 0 && len(values) != 0 {
						limit, err := strconv.ParseInt(values[0], 10, 64)
						if err != nil {
							return fmt.Errorf("Invalid Query Limit: %w", err)
						}
						out.Limit = limit
					}
				case pedia.SearchLabelOffset:
					if out.Continue == "" && len(values) != 0 {
						out.Continue = values[0]
					}

					if out.Continue != "" {
						_, err := strconv.ParseInt(out.Continue, 10, 64)
						if err != nil {
							return fmt.Errorf("Invalid Query Offset(%s): %w", out.Continue, err)
						}
					}
				case pedia.SearchLabelWithContinue:
					if in.WithContinue == nil && len(values) != 0 {
						if err := runtime.Convert_Slice_string_To_Pointer_bool(&values, &out.WithContinue, s); err != nil {
							return err
						}
					}
				case pedia.SearchLabelWithRemainingCount:
					if in.WithRemainingCount == nil && len(values) != 0 {
						if err := runtime.Convert_Slice_string_To_Pointer_bool(&values, &out.WithRemainingCount, s); err != nil {
							return err
						}
					}
				default:
					if strings.Contains(require.Key(), "clusterpedia.io") {
						extraLabelRequest = append(extraLabelRequest, require)
					} else {
						labelRequest = append(labelRequest, require)
					}
				}
			}
		}

		out.LabelSelector = nil
		if len(labelRequest) != 0 {
			out.LabelSelector = labels.NewSelector().Add(labelRequest...)
		}
		if len(extraLabelRequest) != 0 {
			out.ExtraLabelSelector = labels.NewSelector().Add(extraLabelRequest...)
		}
	}
	return nil
}

func Convert_pedia_ListOptions_To_v1alpha1_ListOptions(in *pedia.ListOptions, out *ListOptions, s conversion.Scope) error {
	if err := metainternal.Convert_internalversion_ListOptions_To_v1_ListOptions(&in.ListOptions, &out.ListOptions, s); err != nil {
		return err
	}

	if err := convert_fields_Selector_To_string(&in.EnhancedFieldSelector, &out.FieldSelector, s); err != nil {
		return err
	}

	labels := in.LabelSelector.DeepCopySelector()
	requirements, _ := in.ExtraLabelSelector.Requirements()
	labels.Add(requirements...)
	if err := metav1.Convert_labels_Selector_To_string(&labels, &out.ListOptions.LabelSelector, s); err != nil {
		return err
	}

	out.Owner = in.Owner
	if err := convert_Slice_string_To_String(&in.Names, &out.Names, s); err != nil {
		return err
	}
	if err := convert_Slice_string_To_String(&in.ClusterNames, &out.ClusterNames, s); err != nil {
		return err
	}
	if err := convert_Slice_string_To_String(&in.Namespaces, &out.Namespaces, s); err != nil {
		return err
	}
	if err := convert_pedia_Slice_orderby_To_String(&in.OrderBy, &out.OrderBy, s); err != nil {
		return err
	}

	out.WithContinue = in.WithContinue
	out.WithRemainingCount = in.WithRemainingCount
	return nil
}

func Convert_url_Values_To_v1alpha1_ListOptions(in *url.Values, out *ListOptions, s conversion.Scope) error {
	if err := metav1.Convert_url_Values_To_v1_ListOptions(in, &out.ListOptions, s); err != nil {
		return err
	}

	return autoConvert_url_Values_To_v1alpha1_ListOptions(in, out, s)
}

func convert_String_To_Slice_string(in *string, out *[]string, scope conversion.Scope) error {
	str := strings.TrimSpace(*in)
	if str == "" {
		*out = nil
		return nil
	}

	*out = strings.Split(str, ",")
	return nil
}

func convert_Slice_string_To_String(in *[]string, out *string, scope conversion.Scope) error {
	if len(*in) == 0 {
		*out = ""
		return nil
	}
	*out = strings.Join(*in, ",")
	return nil
}

func convert_Slice_string_To_pedia_Slice_orderby(in *[]string, out *[]pedia.OrderBy, descSep string, s conversion.Scope) error {
	if len(*in) == 0 {
		return nil
	}

	for _, o := range *in {
		sli := strings.Split(strings.TrimSpace(o), descSep)
		switch len(sli) {
		case 0:
			continue
		case 1:
			*out = append(*out, pedia.OrderBy{Field: sli[0]})
			continue
		default:
		}

		var desc bool
		if sli[len(sli)-1] == "desc" {
			desc = true
			sli = sli[:len(sli)-1]
		}

		// if descSep is " ", `orderby` can only be 'field' or 'field desc'
		// example invalid `orderby`: 'field1 field2', 'field1 field2 desc'
		if descSep == " " && len(sli) > 1 {
			return errors.New("Invalid Query OrderBy")
		}

		field := strings.Join(sli, descSep)
		*out = append(*out, pedia.OrderBy{Field: field, Desc: desc})
	}
	return nil
}

func convert_pedia_Slice_orderby_To_String(in *[]pedia.OrderBy, out *string, s conversion.Scope) error {
	if len(*in) == 0 {
		return nil
	}

	sliOrderBy := make([]string, len(*in))
	for _, orderby := range *in {
		str := orderby.Field
		if orderby.Desc {
			str += " desc"
		}
	}

	if err := convert_Slice_string_To_String(&sliOrderBy, out, s); err != nil {
		return err
	}
	return nil
}

func compileErrorOnMissingConversion() {}

func convert_string_To_fields_Selector(in *string, out *fields.Selector, s conversion.Scope) error {
	selector, err := fields.Parse(*in)
	if err != nil {
		return err
	}
	*out = selector
	return nil
}

func convert_fields_Selector_To_string(in *fields.Selector, out *string, s conversion.Scope) error {
	if *in == nil {
		return nil
	}
	*out = (*in).String()
	return nil
}
