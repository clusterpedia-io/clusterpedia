package v1beta1

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	metainternal "k8s.io/apimachinery/pkg/apis/meta/internalversion"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	metafields "k8s.io/apimachinery/pkg/fields"

	"github.com/clusterpedia-io/api/clusterpedia"
	"github.com/clusterpedia-io/api/clusterpedia/fields"
)

func Convert_v1beta1_ListOptions_To_clusterpedia_ListOptions(in *ListOptions, out *clusterpedia.ListOptions, s conversion.Scope) error {
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
	if err := convert_Slice_string_To_clusterpedia_Slice_orderby(&orderbys, &out.OrderBy, " ", s); err != nil {
		return err
	}

	out.OwnerUID = in.OwnerUID
	out.OwnerName = in.OwnerName
	if in.OwnerGroupResource != "" {
		out.OwnerGroupResource = schema.ParseGroupResource(in.OwnerGroupResource)
	}
	out.OwnerSeniority = in.OwnerSeniority

	if err := convert_String_To_Pointer_metav1_Time(&in.Since, &out.Since, nil); err != nil {
		return err
	}

	if err := convert_String_To_Pointer_metav1_Time(&in.Before, &out.Before, nil); err != nil {
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
				case clusterpedia.SearchLabelNames:
					if len(out.Names) == 0 && len(values) != 0 {
						out.Names = values
					}
				case clusterpedia.SearchLabelClusters:
					if len(out.ClusterNames) == 0 && len(values) != 0 {
						out.ClusterNames = values
					}
				case clusterpedia.SearchLabelNamespaces:
					if len(out.Namespaces) == 0 && len(values) != 0 {
						out.Namespaces = values
					}
				case clusterpedia.SearchLabelOwnerUID:
					if out.OwnerUID == "" && len(values) == 1 {
						out.OwnerUID = values[0]
					}
				case clusterpedia.SearchLabelOwnerName:
					if out.OwnerName == "" && len(values) == 1 {
						out.OwnerName = values[0]
					}
				case clusterpedia.SearchLabelOwnerGroupResource:
					if out.OwnerGroupResource.Empty() && len(values) == 1 {
						out.OwnerGroupResource = schema.ParseGroupResource(values[0])
					}
				case clusterpedia.SearchLabelOwnerSeniority:
					if out.OwnerSeniority == 0 && len(values) == 1 {
						seniority, err := strconv.Atoi(values[0])
						if err != nil {
							return fmt.Errorf("Invalid Query OwnerSeniority(%s): %w", values[0], err)
						}
						out.OwnerSeniority = seniority
					}
				case clusterpedia.SearchLabelSince:
					if out.Since == nil && len(values) == 1 {
						if err := convert_String_To_Pointer_metav1_Time(&values[0], &out.Since, nil); err != nil {
							return fmt.Errorf("Invalid Query Since(%s): %w", values[0], err)
						}
					}
				case clusterpedia.SearchLabelBefore:
					if out.Before == nil && len(values) == 1 {
						if err := convert_String_To_Pointer_metav1_Time(&values[0], &out.Before, nil); err != nil {
							return fmt.Errorf("Invalid Query Before(%s): %w", values[0], err)
						}
					}
				case clusterpedia.SearchLabelOrderBy:
					if len(out.OrderBy) == 0 && len(values) != 0 {
						if err := convert_Slice_string_To_clusterpedia_Slice_orderby(&values, &out.OrderBy, "_", s); err != nil {
							return err
						}
					}
				case clusterpedia.SearchLabelLimit:
					if out.Limit == 0 && len(values) != 0 {
						limit, err := strconv.ParseInt(values[0], 10, 64)
						if err != nil {
							return fmt.Errorf("Invalid Query Limit: %w", err)
						}
						out.Limit = limit
					}
				case clusterpedia.SearchLabelOffset:
					if out.Continue == "" && len(values) != 0 {
						out.Continue = values[0]
					}

					if out.Continue != "" {
						_, err := strconv.ParseInt(out.Continue, 10, 64)
						if err != nil {
							return fmt.Errorf("Invalid Query Offset(%s): %w", out.Continue, err)
						}
					}
				case clusterpedia.SearchLabelWithContinue:
					if in.WithContinue == nil && len(values) != 0 {
						if err := runtime.Convert_Slice_string_To_Pointer_bool(&values, &out.WithContinue, s); err != nil {
							return err
						}
					}
				case clusterpedia.SearchLabelWithRemainingCount:
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
	if out.Before.Before(out.Since) {
		return fmt.Errorf("Invalid Query, Since is after Before")
	}
	if len(in.urlQuery) > 0 {
		// Out URLQuery will not be modified, so deepcopy is not used here.
		out.URLQuery = in.urlQuery
	}

	out.OnlyMetadata = in.OnlyMetadata
	return nil
}

func Convert_clusterpedia_ListOptions_To_v1beta1_ListOptions(in *clusterpedia.ListOptions, out *ListOptions, s conversion.Scope) error {
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

	out.OwnerUID = in.OwnerUID
	out.OwnerName = in.OwnerName
	out.OwnerGroupResource = in.OwnerGroupResource.String()
	out.OwnerSeniority = in.OwnerSeniority

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

func Convert_url_Values_To_v1beta1_ListOptions(in *url.Values, out *ListOptions, s conversion.Scope) error {
	if err := metav1.Convert_url_Values_To_v1_ListOptions(in, &out.ListOptions, s); err != nil {
		return err
	}
	// Save the native query parameters for use by listoptions.
	out.urlQuery = *in

	return autoConvert_url_Values_To_v1beta1_ListOptions(in, out, s)
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

func convert_String_To_Pointer_metav1_Time(in *string, out **metav1.Time, scope conversion.Scope) error {
	str := strings.TrimSpace(*in)
	if len(str) == 0 {
		return nil
	}

	var err error
	var t time.Time
	switch {
	case strings.Contains(str, "T"):
		// If the query parameter contains "+", it will be parsed into " ".
		// The query parameter need to be encoded.
		t, err = time.Parse(time.RFC3339, *in)
	case strings.Contains(str, " "):
		t, err = time.Parse("2006-01-02 15:04:05", *in)
	case strings.Contains(str, "-"):
		t, err = time.Parse("2006-01-02", *in)
	default:
		var timestamp int64
		timestamp, err = strconv.ParseInt(*in, 10, 64)
		if err != nil {
			break
		}

		switch len(str) {
		case 10:
			t = time.Unix(timestamp, 0)
		case 13:
			t = time.Unix(timestamp/1e3, (timestamp%1e3)*1e6)
		default:
			return errors.New("Invalid timestamp: only timestamps with string lengths of 10(as s) and 13(as ms) are supported")
		}
	}
	if err != nil {
		return fmt.Errorf("Invalid datetime: %s, a valid datetime format: RFC3339, Datetime(2006-01-02 15:04:05), Date(2006-01-02), Unix Timestamp", *in)
	}
	*out = &metav1.Time{Time: t}
	return nil
}

func convert_Slice_string_To_clusterpedia_Slice_orderby(in *[]string, out *[]clusterpedia.OrderBy, descSep string, s conversion.Scope) error {
	if len(*in) == 0 {
		return nil
	}

	for _, o := range *in {
		sli := strings.Split(strings.TrimSpace(o), descSep)
		switch len(sli) {
		case 0:
			continue
		case 1:
			*out = append(*out, clusterpedia.OrderBy{Field: sli[0]})
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
		*out = append(*out, clusterpedia.OrderBy{Field: field, Desc: desc})
	}
	return nil
}

func convert_pedia_Slice_orderby_To_String(in *[]clusterpedia.OrderBy, out *string, s conversion.Scope) error {
	if len(*in) == 0 {
		return nil
	}

	sliOrderBy := make([]string, len(*in))
	for i, orderby := range *in {
		str := orderby.Field
		if orderby.Desc {
			str += " desc"
		}
		sliOrderBy[i] = str
	}

	if err := convert_Slice_string_To_String(&sliOrderBy, out, s); err != nil {
		return err
	}
	return nil
}

func convert_string_To_fields_Selector(in *string, out *fields.Selector, s conversion.Scope) error {
	selector, err := fields.Parse(*in)
	if err != nil {
		return err
	}
	*out = selector
	return nil
}

func Convert_EnhancedFieldSelector_To_FieldSelector(in *fields.Selector) (*metafields.Selector, error) {
	out := ""
	result := metafields.Nothing()
	if err := convert_fields_Selector_To_string(in, &out, nil); err != nil {
		return nil, err
	}
	if err := metav1.Convert_string_To_fields_Selector(&out, &result, nil); err != nil {
		return nil, err
	}
	return &result, nil
}

func convert_fields_Selector_To_string(in *fields.Selector, out *string, s conversion.Scope) error {
	if *in == nil {
		return nil
	}
	*out = (*in).String()
	return nil
}

// nolint:unused
func compileErrorOnMissingConversion() {}
