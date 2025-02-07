package resourcebuilder

import (
	"fmt"

	appv1 "github.com/unbindapp/unbind-operator/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// Resourcebuilder is responsible for building native k8s resources
type ResourceBuilder struct {
	app    *appv1.App
	scheme *runtime.Scheme
}

func NewResourceBuilder(app *appv1.App, scheme *runtime.Scheme) *ResourceBuilder {
	return &ResourceBuilder{
		app:    app,
		scheme: scheme,
	}
}

func (rb *ResourceBuilder) buildObjectMeta() metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      rb.app.Name,
		Namespace: rb.app.Namespace,
		Labels:    rb.buildLabels(),
	}
}

func (rb *ResourceBuilder) buildLabels() map[string]string {
	return map[string]string{
		"app":                          rb.app.Name,
		"app.kubernetes.io/name":       rb.app.Name,
		"app.kubernetes.io/instance":   rb.app.Name,
		"app.kubernetes.io/managed-by": "unbind-operator",
	}
}

func (rb *ResourceBuilder) buildPodAnnotations() map[string]string {
	return map[string]string{}
}

func (rb *ResourceBuilder) buildIngressAnnotations() map[string]string {
	return map[string]string{
		"kubernetes.io/ingress.class":                        "nginx",
		"cert-manager.io/cluster-issuer":                     "letsencrypt-prod",
		"nginx.ingress.kubernetes.io/eventsource":            "true",
		"nginx.ingress.kubernetes.io/add-base-url":           "true",
		"nginx.ingress.kubernetes.io/ssl-redirect":           "true",
		"nginx.ingress.kubernetes.io/websocket-services":     fmt.Sprintf("%s-service", rb.app.Name),
		"nginx.ingress.kubernetes.io/proxy-send-timeout":     "1800",
		"nginx.ingress.kubernetes.io/proxy-read-timeout":     "21600",
		"nginx.ingress.kubernetes.io/proxy-body-size":        "10m",
		"nginx.ingress.kubernetes.io/upstream-hash-by":       "$realip_remote_addr",
		"nginx.ingress.kubernetes.io/affinity":               "cookie",
		"nginx.ingress.kubernetes.io/session-cookie-name":    fmt.Sprintf("%s-session", rb.app.Name),
		"nginx.ingress.kubernetes.io/session-cookie-expires": "172800",
		"nginx.ingress.kubernetes.io/session-cookie-max-age": "172800",
	}
}

// ! TODO - these need to be configurable in our app spec
// func (rb *ResourceBuilder) buildResourceRequirements() corev1.ResourceRequirements {
// 	return corev1.ResourceRequirements{
// 		Requests: corev1.ResourceList{
// 			corev1.ResourceCPU:    resource.MustParse("100m"),
// 			corev1.ResourceMemory: resource.MustParse("128Mi"),
// 		},
// 		Limits: corev1.ResourceList{
// 			corev1.ResourceCPU:    resource.MustParse("500m"),
// 			corev1.ResourceMemory: resource.MustParse("512Mi"),
// 		},
// 	}
// }

// func (rb *ResourceBuilder) buildLivenessProbe(port int32) *corev1.Probe {
// 	return &corev1.Probe{
// 		ProbeHandler: corev1.ProbeHandler{
// 			HTTPGet: &corev1.HTTPGetAction{
// 				Path: "/healthz",
// 				Port: intstr.FromInt32(port),
// 			},
// 		},
// 		InitialDelaySeconds: 30,
// 		PeriodSeconds:       10,
// 		TimeoutSeconds:      5,
// 		FailureThreshold:    3,
// 	}
// }

// func (rb *ResourceBuilder) buildReadinessProbe(port int32) *corev1.Probe {
// 	return &corev1.Probe{
// 		ProbeHandler: corev1.ProbeHandler{
// 			HTTPGet: &corev1.HTTPGetAction{
// 				Path: "/readyz",
// 				Port: intstr.FromInt32(port),
// 			},
// 		},
// 		InitialDelaySeconds: 5,
// 		PeriodSeconds:       5,
// 		TimeoutSeconds:      3,
// 		FailureThreshold:    2,
// 	}
// }
