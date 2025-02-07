package resourcebuilder

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// Build kubernetes Deployment objects
func (rb *ResourceBuilder) BuildDeployment() (*appsv1.Deployment, error) {
	// ! TODO - configurable probes and resources

	deployment := &appsv1.Deployment{
		ObjectMeta: rb.buildObjectMeta(),
		Spec: appsv1.DeploymentSpec{
			Replicas: rb.app.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: rb.buildLabels(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      rb.buildLabels(),
					Annotations: rb.buildPodAnnotations(),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "app",
						Image: rb.app.Spec.Image,
						Ports: []corev1.ContainerPort{{
							// ! TODO - don't deref this if nil
							ContainerPort: *rb.app.Spec.Port,
						}},
						// Resources:      rb.buildResourceRequirements(),
						// LivenessProbe:  rb.buildLivenessProbe(port),
						// ReadinessProbe: rb.buildReadinessProbe(port),
					}},
				},
			},
		},
	}

	if err := ctrl.SetControllerReference(rb.app, deployment, rb.scheme); err != nil {
		return nil, fmt.Errorf("setting controller reference: %w", err)
	}

	return deployment, nil
}
