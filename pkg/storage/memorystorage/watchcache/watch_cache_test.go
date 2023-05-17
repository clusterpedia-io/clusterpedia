package watchcache

import (
	"testing"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
)

func Test_filterLabelSelector(t *testing.T) {
	type request struct {
		labelSelector labels.Selector
		label         labels.Set
	}
	var tests = []struct {
		name   string
		req    request
		before func(req *request)
		want   bool
	}{
		{
			name: "empty label selector",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{
				labelSelector: nil,
				label:         labels.Set{},
			},
			want: false,
		},
		{
			name: "empty label",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{
				labelSelector: labels.Everything(),
				label:         nil,
			},
			want: true,
		},
		{
			name: "lable exist operator,but exist",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.Exists, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: false,
		},
		{
			name: "lable exist operator,but not exist",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("app", selection.Exists, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: true,
		},
		{
			name: "lable not exist operator,but exist",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.DoesNotExist, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: true,
		},
		{
			name: "lable not exist operator,but not exist",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("app", selection.DoesNotExist, nil)
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: false,
		},
		{
			name: "lable Equals operator,but not equals",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.Equals, []string{"prod"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: true,
		},
		{
			name: "lable Equals operator,but equals",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.Equals, []string{"dev"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: false,
		},
		{
			name: "lable NotEquals operator,but equals",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.NotEquals, []string{"dev"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: true,
		},
		{
			name: "lable NotEquals operator,but not equals",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.NotEquals, []string{"prod"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: false,
		},
		{
			name: "lable IN operator,but not in",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.In, []string{"prod", "test"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: true,
		},
		{
			name: "lable IN operator,but in",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{

				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.In, []string{"prod", "test", "dev"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: false,
		},
		{
			name: "lable NotIn operator,but in",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{
				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.NotIn, []string{"prod", "test", "dev"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: true,
		},
		{
			name: "lable NotIn operator,but not in",
			req: struct {
				labelSelector labels.Selector
				label         labels.Set
			}{
				labelSelector: labels.NewSelector(),
				label: labels.Set{
					"env": "dev",
				},
			},
			before: func(req *request) {
				requirement, err := labels.NewRequirement("env", selection.NotIn, []string{"prod", "test"})
				if err != nil {
					t.Fatal(err)
				}
				req.labelSelector = req.labelSelector.Add(labels.Requirements{*requirement}...)
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.before != nil {
				tt.before(&tt.req)
			}
			if got := filterLabelSelector(tt.req.labelSelector, tt.req.label); got != tt.want {
				t.Errorf("filterLabelSelector() = %v, want %v", got, tt.want)
			}
		})
	}
}
