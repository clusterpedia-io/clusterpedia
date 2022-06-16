package negotiation

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	metav1beta1 "k8s.io/apimachinery/pkg/apis/meta/v1beta1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apiserver/pkg/endpoints/handlers/negotiation"
)

var (
	TableEndpointRestrictions                 negotiation.EndpointRestrictions = defaultEndpointRestrictions{allowTable: true}
	PartialObjectMetadataEndpointRestrictions negotiation.EndpointRestrictions = defaultEndpointRestrictions{allowPartialObjectMetadata: true}
	DefaultEndpointRestrictions               negotiation.EndpointRestrictions = defaultEndpointRestrictions{allowTable: true, allowPartialObjectMetadata: true}
)

type defaultEndpointRestrictions struct {
	allowTable                 bool
	allowPartialObjectMetadata bool
}

func (r defaultEndpointRestrictions) AllowsMediaTypeTransform(mimeType, mimeSubType string, gvk *schema.GroupVersionKind) bool {
	if gvk == nil {
		return false
	}

	if gvk.GroupVersion() != metav1beta1.SchemeGroupVersion && gvk.GroupVersion() != metav1.SchemeGroupVersion {
		return false
	}

	switch gvk.Kind {
	case "Table":
		return r.allowTable && mimeType == "application" && (mimeSubType == "json" || mimeSubType == "yaml")
	case "PartialObjectMetadata", "PartialObjectMetadataList":
		return r.allowPartialObjectMetadata
	}
	return false
}

func (r defaultEndpointRestrictions) AllowsServerVersion(version string) bool {
	return false
}

func (r defaultEndpointRestrictions) AllowsStreamSchema(s string) bool {
	return s == "watch"
}

var NegotiateMediaTypeOptions = negotiation.NegotiateMediaTypeOptions
