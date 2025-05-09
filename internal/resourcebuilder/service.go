package resourcebuilder

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Builder for kubernetes Service objects
var ErrServiceNotNeeded = fmt.Errorf("service not needed, probably no ports configured")

func (rb *ResourceBuilder) BuildService() (*corev1.Service, error) {
	if len(rb.service.Spec.Config.Ports) < 1 {
		return nil, ErrServiceNotNeeded
	}

	ports := make([]corev1.ServicePort, len(rb.service.Spec.Config.Ports))
	for i, port := range rb.service.Spec.Config.Ports {
		protocol := corev1.ProtocolTCP
		if port.Protocol != nil {
			protocol = *port.Protocol
		}
		ports[i] = corev1.ServicePort{
			Name:       strings.ToLower(fmt.Sprintf("%s-%d-%s", rb.service.Spec.Name, port.Port, protocol)),
			Protocol:   protocol,
			Port:       port.Port,
			TargetPort: intstr.FromInt32(port.Port),
		}
	}

	service := &corev1.Service{
		ObjectMeta: rb.buildObjectMeta(),
		Spec: corev1.ServiceSpec{
			Selector: rb.getLabelSelectors(),
			Ports:    ports,
		},
	}
	return service, nil
}
