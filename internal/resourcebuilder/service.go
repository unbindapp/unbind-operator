package resourcebuilder

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// Builder for kubernetes Service objects
var ErrServiceNotNeeded = fmt.Errorf("service not needed, probably no ports configured")

// BuildServices builds kubernetes Service objects, creating NodePort services where specified
func (rb *ResourceBuilder) BuildServices() ([]*corev1.Service, error) {
	if len(rb.service.Spec.Config.Ports) < 1 {
		return nil, ErrServiceNotNeeded
	}

	// Group ports by service type (NodePort vs ClusterIP)
	nodePortPorts := []corev1.ServicePort{}
	clusterIPPorts := []corev1.ServicePort{}

	for _, port := range rb.service.Spec.Config.Ports {
		protocol := corev1.ProtocolTCP
		if port.Protocol != nil {
			protocol = *port.Protocol
		}

		servicePort := corev1.ServicePort{
			Name:       strings.ToLower(fmt.Sprintf("%s-%d-%s", rb.service.Spec.Name, port.Port, protocol)),
			Protocol:   protocol,
			Port:       port.Port,
			TargetPort: intstr.FromInt32(port.Port),
		}

		// If NodePort is specified, add it to the nodePortPorts slice
		if port.NodePort != nil {
			servicePort.NodePort = *port.NodePort
			nodePortPorts = append(nodePortPorts, servicePort)
		} else {
			clusterIPPorts = append(clusterIPPorts, servicePort)
		}
	}

	services := []*corev1.Service{}

	// Create NodePort service if needed
	if len(nodePortPorts) > 0 {
		nodePortService := &corev1.Service{
			ObjectMeta: rb.buildObjectMeta(),
			Spec: corev1.ServiceSpec{
				Type:     corev1.ServiceTypeNodePort,
				Selector: rb.getLabelSelectors(),
				Ports:    nodePortPorts,
			},
		}
		// Add a suffix to distinguishnodeport service
		nodePortService.ObjectMeta.Name = nodePortService.ObjectMeta.Name + "-nodeport"
		services = append(services, nodePortService)
	}

	// Create ClusterIP service if needed
	if len(clusterIPPorts) > 0 {
		clusterIPService := &corev1.Service{
			ObjectMeta: rb.buildObjectMeta(),
			Spec: corev1.ServiceSpec{
				Selector: rb.getLabelSelectors(),
				Ports:    clusterIPPorts,
			},
		}
		services = append(services, clusterIPService)
	}

	return services, nil
}
