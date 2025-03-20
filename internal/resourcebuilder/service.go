package resourcebuilder

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Builder for kubernetes Service objects
var ErrServiceNotNeeded = fmt.Errorf("service not needed, probably no ports configured")

func (rb *ResourceBuilder) BuildService() (*corev1.Service, error) {
	if rb.service.Spec.Config.Port == nil {
		return nil, ErrServiceNotNeeded
	}

	service := &corev1.Service{
		ObjectMeta: rb.buildObjectMeta(),
		Spec: corev1.ServiceSpec{
			Selector: rb.getCommonLabels(),
			Ports: []corev1.ServicePort{{
				Protocol: corev1.ProtocolTCP,
				// ! TODO - don't deref this if nil
				Port:       *rb.service.Spec.Config.Port,
				TargetPort: intstr.FromInt32(*rb.service.Spec.Config.Port),
			}},
		},
	}

	if err := ctrl.SetControllerReference(rb.service, service, rb.scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	return service, nil
}
