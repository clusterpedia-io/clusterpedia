package v1alpha1

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"strings"

	"github.com/Masterminds/sprig/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	PolicyValidatedCondition   = "Validated"
	PolicyReconcilingCondition = "Reconciling"

	LifecycleCreatedCondition  = "Created"
	LifecycleUpdatingCondition = "Updating"
)

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:printcolumn:name="Validated",type=string,JSONPath=".status.conditions[?(@.type == 'Validated')].reason"
// +kubebuilder:printcolumn:name="Reconciling",type=string,JSONPath=".status.conditions[?(@.type == 'Reconciling')].reason"
type ClusterImportPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ClusterImportPolicySpec `json:"spec,omitempty"`

	// +optional
	Status ClusterImportPolicyStatus `json:"status,omitmepty"`
}

type ClusterImportPolicySpec struct {
	// +required
	// +kubebuilder:validation:Required
	Source SourceType `json:"source"`

	// +optional
	// +listType=map
	// +listMapKey=key
	References []IntendReferenceResourceTemplate `json:"references,omitempty"`

	// +required
	// +kubebuilder:validation:Required
	NameTemplate LifecycleNameTemplate `json:"nameTemplate"`

	Policy `json:",inline"`
}

type LifecycleNameTemplate string

func (t LifecycleNameTemplate) Template() (*template.Template, error) {
	return newTemplate("lifecycle-name", string(t))
}

type ClusterImportPolicyStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:subresource:status
// +kubebuilder:resource:scope="Cluster"
// +kubebuilder:printcolumn:name="Created",type=string,JSONPath=".status.conditions[?(@.type == 'Created')].reason"
// +kubebuilder:printcolumn:name="Updating",type=string,JSONPath=".status.conditions[?(@.type == 'Updating')].reason"
type PediaClusterLifecycle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec PediaClusterLifecycleSpec `json:"spec,omitempty"`

	// +optional
	Status PediaClusterLifecycleStatus `json:"status,omitempty"`
}

type PediaClusterLifecycleSpec struct {
	// +required
	// +kubebuilder:validation:Required
	Source DependentResource `json:"source"`

	// +optional
	// +listType=map
	// +listMapKey=key
	References []ReferenceResourceTemplate `json:"references,omitempty"`

	Policy `json:",inline"`
}

type PediaClusterLifecycleStatus struct {
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	References []DependentResource `json:"references"`
}

type Policy struct {
	// +required
	// +kubebuilder:validation:Required
	Template string `json:"template"`

	// +required
	// +kubebuilder:validation:Required
	CreationCondition string `json:"creationCondition"`

	/*
		// +required
		// +kubebuilder:validation:Required
		UpdateTemplate string `json:"updateTemplate,omitempty"`

		// +optional
		DeletionCondition string `json:"deletionCondition,omitempty"`
	*/
}

func (policy Policy) Validate() (errs []error) {
	if _, err := newTemplate("", policy.Template); err != nil {
		errs = append(errs, err)
	}
	if _, err := newTemplate("", policy.CreationCondition); err != nil {
		errs = append(errs, err)
	}

	return errs
}

func (policy Policy) CouldCreate(writer *bytes.Buffer, data interface{}) (bool, error) {
	tmpl, err := newTemplate("creationcondition", policy.CreationCondition)
	if err != nil {
		return false, err
	}

	writer.Reset()
	if err := tmpl.Execute(writer, data); err != nil {
		return false, err
	}
	return strings.TrimSpace(strings.ToLower(replaceNoValue(writer.String()))) == "true", nil
}

func (policy Policy) ResolvePediaCluster(writer *bytes.Buffer, data interface{}) ([]byte, error) {
	tmpl, err := newTemplate("pediacluster", policy.Template)
	if err != nil {
		return nil, err
	}

	writer.Reset()
	if err := tmpl.Execute(writer, data); err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(writer.Bytes(), []byte("<no value>"), []byte("")), nil
}

type BaseReferenceResourceTemplate struct {
	// +required
	// +kubebuilder:validation:Required
	Key string `json:"key"`

	// +required
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// +required
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`

	// +optional
	NamespaceTemplate string `json:"namespaceTemplate,omitempty"`

	// +required
	// +kubebuilder:validation:Required
	NameTemplate string `json:"nameTemplate"`
}

func (ref BaseReferenceResourceTemplate) GroupResource() schema.GroupResource {
	return schema.GroupResource{Group: ref.Group, Resource: ref.Resource}
}

func (ref BaseReferenceResourceTemplate) ResolveNamespaceAndName(writer *bytes.Buffer, data interface{}) (namespace, name string, err error) {
	if ref.NamespaceTemplate != "" {
		tmpl, err := newTemplate("references", ref.NamespaceTemplate)
		if err != nil {
			return "", "", fmt.Errorf("namespace: %w", err)
		}
		writer.Reset()
		if err := tmpl.Execute(writer, data); err != nil {
			return "", "", fmt.Errorf("namespace: %w", err)
		}
		namespace = replaceNoValue(writer.String())
	}

	tmpl, err := newTemplate("references", ref.NameTemplate)
	if err != nil {
		return "", "", fmt.Errorf("name: %w", err)
	}
	writer.Reset()
	if err := tmpl.Execute(writer, data); err != nil {
		return "", "", fmt.Errorf("name: %w", err)
	}
	name = replaceNoValue(writer.String())
	return
}

type IntendReferenceResourceTemplate struct {
	BaseReferenceResourceTemplate `json:",inline"`

	// +optional
	Versions []string `json:"versions,omitempty"`
}

type ReferenceResourceTemplate struct {
	BaseReferenceResourceTemplate `json:",inline"`

	// +required
	// +kubebuilder:validation:Required
	Version string `json:"version"`
}

func (r ReferenceResourceTemplate) String() string {
	strs := []string{r.Group, r.Version, r.Resource}
	if r.NamespaceTemplate != "" {
		strs = append(strs, r.NamespaceTemplate)
	}
	strs = append(strs, r.NameTemplate)
	return strings.Join(strs, "/")
}

func (r ReferenceResourceTemplate) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
}

func (ref ReferenceResourceTemplate) Validate(errs []error) {
	if ref.Key == "" {
		errs = append(errs, errors.New("reference resource key is required"))
	}

	if ref.NameTemplate == "" {
		errs = append(errs, errors.New("reference resource name is required"))
	}
	return
}

func (ref ReferenceResourceTemplate) Resolve(writer *bytes.Buffer, data interface{}) (DependentResource, error) {
	namespace, name, err := ref.ResolveNamespaceAndName(writer, data)
	if err != nil {
		return DependentResource{}, err
	}
	return DependentResource{Group: ref.Group, Version: ref.Version, Resource: ref.Resource,
		Namespace: namespace, Name: name}, nil
}

type SourceType struct {
	// +required
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// +optional
	Versions []string `json:"versions,omitempty"`

	// +required
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`

	// +optional
	SelectorTemplate SelectorTemplate `json:"selectorTemplate,omitempty"`
}

func (st SourceType) GroupResource() schema.GroupResource {
	return schema.GroupResource{Group: st.Group, Resource: st.Resource}
}

type SelectorTemplate string

func (t SelectorTemplate) Template() (*template.Template, error) {
	return newTemplate("select-source", string(t))
}

type DependentResource struct {
	// +required
	// +kubebuilder:validation:Required
	Group string `json:"group"`

	// +required
	// +kubebuilder:validation:Required
	Version string `json:"version"`

	// +required
	// +kubebuilder:validation:Required
	Resource string `json:"resource"`

	// +optional
	Namespace string `json:"namespace"`

	// +required
	// +kubebuilder:validation:Required
	Name string `json:"name"`
}

func (r DependentResource) GroupVersionResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: r.Group, Version: r.Version, Resource: r.Resource}
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ClusterImportPolicyList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterImportPolicy `json:"items"`
}

// +kubebuilder:object:root=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type PediaClusterLifecycleList struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []PediaClusterLifecycle `json:"items"`
}

func newTemplate(name string, tmpltext string) (*template.Template, error) {
	return template.New(name).Funcs(sprig.FuncMap()).Parse(tmpltext)
}

func replaceNoValue(value string) string {
	return strings.ReplaceAll(value, "<no value>", "")
}
