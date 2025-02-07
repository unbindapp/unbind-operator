package resourcebuilder

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Builder for kubernetes Service objects
func (rb *ResourceBuilder) BuildService() (*corev1.Service, error) {
	service := &corev1.Service{
		ObjectMeta: rb.buildObjectMeta(),
		Spec: corev1.ServiceSpec{
			Selector: rb.buildLabels(),
			Ports: []corev1.ServicePort{{
				Protocol: corev1.ProtocolTCP,
				// ! TODO - don't deref this if nil
				Port:       *rb.app.Spec.Port,
				TargetPort: intstr.FromInt32(*rb.app.Spec.Port),
			}},
		},
	}

	if err := ctrl.SetControllerReference(rb.app, service, rb.scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	return service, nil
}
