package resourcebuilder

import (
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrIngressNotNeeded = fmt.Errorf("ingress not needed, probably no domain configured")

func (rb *ResourceBuilder) BuildIngress() (*networkingv1.Ingress, error) {
	if rb.service.Spec.Config.Host == nil || rb.service.Spec.Config.Port == nil || !rb.service.Spec.Config.Public || *rb.service.Spec.Config.Host == "" {
		return nil, ErrIngressNotNeeded
	}

	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        rb.service.Name,
			Namespace:   rb.service.Namespace,
			Labels:      rb.getCommonLabels(),
			Annotations: rb.buildIngressAnnotations(),
		},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{*rb.service.Spec.Config.Host},
				SecretName: fmt.Sprintf("%s-tls-secret", strings.ToLower(rb.service.Name)),
			}},
			Rules: []networkingv1.IngressRule{{
				Host: *rb.service.Spec.Config.Host,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: rb.service.Name,
									Port: networkingv1.ServiceBackendPort{
										Number: *rb.service.Spec.Config.Port,
									},
								},
							},
						}},
					},
				},
			}},
		},
	}

	return ingress, nil
}
