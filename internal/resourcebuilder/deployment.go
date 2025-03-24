package resourcebuilder

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Build kubernetes Deployment objects
func (rb *ResourceBuilder) BuildDeployment() (*appsv1.Deployment, error) {
	replicas := int32(2)
	if rb.service.Spec.Config.Replicas != nil {
		replicas = *rb.service.Spec.Config.Replicas
	}

	var ports []corev1.ContainerPort
	for _, port := range rb.service.Spec.Config.Ports {
		ports = append(ports, corev1.ContainerPort{
			ContainerPort: port.Port,
		})
	}

	// Create the container
	container := corev1.Container{
		Name:  "service",
		Image: rb.service.Spec.Config.Image,
		Ports: ports,
		// Always load service environment
		EnvFrom: []corev1.EnvFromSource{
			{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: rb.service.Spec.KubernetesSecret,
					},
				},
			},
		},
		// ! TODO
		// Resources: rb.buildResourceRequirements(),
		// LivenessProbe: rb.buildLivenessProbe(port),
		// ReadinessProbe: rb.buildReadinessProbe(port),
	}

	// Handle run command if provided
	if rb.service.Spec.Config.RunCommand != nil && *rb.service.Spec.Config.RunCommand != "" {
		// Parse the RunCommand into command and args
		// This uses a simple shell-like split, more complex parsing might be needed
		parts := parseCommand(*rb.service.Spec.Config.RunCommand)

		if len(parts) > 0 {
			container.Command = []string{parts[0]}

			if len(parts) > 1 {
				container.Args = parts[1:]
			}
		}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: rb.buildObjectMeta(),
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: rb.getCommonLabels(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      rb.getCommonLabels(),
					Annotations: rb.buildPodAnnotations(),
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: []corev1.LocalObjectReference{{
						Name: REGISTRY_SECRET_NAME,
					}},
					Containers: []corev1.Container{container},
				},
			},
		},
	}

	return deployment, nil
}

// parseCommand splits a command string into parts, respecting quotes
// This is a simplified version and might need to be enhanced for more complex commands
func parseCommand(cmd string) []string {
	if cmd == "" {
		return []string{}
	}

	// Simple case: split by spaces
	// For more complex parsing that handles quotes and escapes correctly,
	// you might want to use a more sophisticated approach

	var result []string
	var current string
	var inQuotes bool
	var quoteChar rune

	for _, r := range cmd {
		switch {
		case r == '"' || r == '\'':
			if inQuotes && r == quoteChar {
				// End quote
				inQuotes = false
			} else if !inQuotes {
				// Start quote
				inQuotes = true
				quoteChar = r
			} else {
				// Quote character inside different quotes
				current += string(r)
			}
		case r == ' ' && !inQuotes:
			// Space outside quotes - split
			if current != "" {
				result = append(result, current)
				current = ""
			}
		default:
			current += string(r)
		}
	}

	// Add the last part if there is one
	if current != "" {
		result = append(result, current)
	}

	return result
}
