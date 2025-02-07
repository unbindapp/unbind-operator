package resourcebuilder

import (
	"fmt"
	"strings"

	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (rb *ResourceBuilder) BuildIngress() (*networkingv1.Ingress, error) {
	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:        rb.app.Name,
			Namespace:   rb.app.Namespace,
			Labels:      rb.buildLabels(),
			Annotations: rb.buildIngressAnnotations(),
		},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{
				Hosts:      []string{rb.app.Spec.Domain},
				SecretName: fmt.Sprintf("%s-tls-secret", strings.ToLower(rb.app.Name)),
			}},
			Rules: []networkingv1.IngressRule{{
				Host: rb.app.Spec.Domain,
				IngressRuleValue: networkingv1.IngressRuleValue{
					HTTP: &networkingv1.HTTPIngressRuleValue{
						Paths: []networkingv1.HTTPIngressPath{{
							Path:     "/",
							PathType: &pathType,
							Backend: networkingv1.IngressBackend{
								Service: &networkingv1.IngressServiceBackend{
									Name: rb.app.Name,
									Port: networkingv1.ServiceBackendPort{
										// ! TODO - don't deref this if nil
										Number: *rb.app.Spec.Port,
									},
								},
							},
						}},
					},
				},
			}},
		},
	}

	if err := ctrl.SetControllerReference(rb.app, ingress, rb.scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	return ingress, nil
}
