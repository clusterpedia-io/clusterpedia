package watch

import (
	"net/http"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
)

// NewErrorEvent return a watch event of error status with an error
func NewErrorEvent(err error) watch.Event {
	errEvent := watch.Event{Type: watch.Error}
	switch err := err.(type) {
	case runtime.Object:
		errEvent.Object = err
	case *apierrors.StatusError:
		errEvent.Object = &err.ErrStatus
	default:
		errEvent.Object = &metav1.Status{
			Status:  metav1.StatusFailure,
			Message: err.Error(),
			Reason:  metav1.StatusReasonInternalError,
			Code:    http.StatusInternalServerError,
		}
	}
	return errEvent
}
