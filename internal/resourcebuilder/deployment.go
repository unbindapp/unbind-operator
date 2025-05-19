package resourcebuilder

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var ErrDeploymentNotNeeded = fmt.Errorf("deployment not needed, probably no image configured")

// Build kubernetes Deployment objects
func (rb *ResourceBuilder) BuildDeployment() (*appsv1.Deployment, error) {
	if rb.service.Spec.Config.Image == "" {
		return nil, ErrDeploymentNotNeeded
	}

	replicas := int32(2)
	if rb.service.Spec.Config.Replicas != nil {
		replicas = *rb.service.Spec.Config.Replicas
	}

	ports := []corev1.ContainerPort{}
	for _, port := range rb.service.Spec.Config.Ports {
		ports = append(ports, corev1.ContainerPort{
			ContainerPort: port.Port,
		})
	}

	// Create the container
	container := corev1.Container{
		Name:            rb.service.Name,
		Image:           rb.service.Spec.Config.Image,
		ImagePullPolicy: corev1.PullAlways,
		Ports:           ports,
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
		Env:             rb.service.Spec.EnvVars,
		SecurityContext: rb.service.Spec.SecurityContext,
		// ! TODO
		// Resources: rb.buildResourceRequirements(),
	}

	// Add probes if health check is configured
	if rb.service.Spec.Config.HealthCheck != nil {
		container.LivenessProbe = rb.buildLivenessProbe()
		container.ReadinessProbe = rb.buildReadinessProbe()
		container.StartupProbe = rb.buildStartupProbe()
	}

	// Add volume mounts if specified
	var volumes []corev1.Volume
	volumeMounts := []corev1.VolumeMount{}

	// Handle regular volumes
	if len(rb.service.Spec.Config.Volumes) > 0 {
		for _, vol := range rb.service.Spec.Config.Volumes {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      vol.Name,
				MountPath: vol.MountPath,
			})
			volumes = append(volumes, corev1.Volume{
				Name: vol.Name,
				VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
						ClaimName: vol.Name,
					},
				},
			})
		}
	}

	// Handle variable mounts
	if len(rb.service.Spec.Config.VariableMounts) > 0 {
		for i, vm := range rb.service.Spec.Config.VariableMounts {
			volumeName := fmt.Sprintf("var-mount-%d", i)
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: vm.Path,
				SubPath:   vm.Name, // Use the variable name as the subpath
			})
			volumes = append(volumes, corev1.Volume{
				Name: volumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: rb.service.Spec.KubernetesSecret,
						Items: []corev1.KeyToPath{
							{
								Key:  vm.Name,
								Path: vm.Name,
							},
						},
					},
				},
			})
		}
	}

	container.VolumeMounts = volumeMounts

	// Handle run command if provided
	if rb.service.Spec.Config.RunCommand != nil && *rb.service.Spec.Config.RunCommand != "" {
		parsedCommand := parseCommand(*rb.service.Spec.Config.RunCommand)

		// If it's a shell command (detected shell operators)
		if len(parsedCommand) >= 3 && parsedCommand[0] == "/bin/sh" && parsedCommand[1] == "-c" {
			container.Command = parsedCommand
			// No args needed as they're included in the shell command
		} else if len(parsedCommand) > 0 {
			// Regular command with args
			container.Command = []string{parsedCommand[0]}
			if len(parsedCommand) > 1 {
				container.Args = parsedCommand[1:]
			}
		}
	}

	// Make pull secrets
	imagePullSecrets := make([]corev1.LocalObjectReference, len(rb.service.Spec.ImagePullSecrets))
	for i, secret := range rb.service.Spec.ImagePullSecrets {
		imagePullSecrets[i] = corev1.LocalObjectReference{
			Name: secret,
		}
	}

	// Build init containers if specified
	var initContainers []corev1.Container
	if len(rb.service.Spec.Config.InitContainers) > 0 {
		for i, ic := range rb.service.Spec.Config.InitContainers {
			initContainer := corev1.Container{
				Name:            fmt.Sprintf("%s-init-%d", rb.service.Name, i),
				Image:           ic.Image,
				ImagePullPolicy: corev1.PullAlways,
				VolumeMounts:    volumeMounts, // Share the same volume mounts as the main container
				EnvFrom: []corev1.EnvFromSource{
					{
						SecretRef: &corev1.SecretEnvSource{
							LocalObjectReference: corev1.LocalObjectReference{
								Name: rb.service.Spec.KubernetesSecret,
							},
						},
					},
				},
				Env: rb.service.Spec.EnvVars,
			}

			// Parse and set command if provided
			if ic.Command != "" {
				parsedCommand := parseCommand(ic.Command)
				if len(parsedCommand) >= 3 && parsedCommand[0] == "/bin/sh" && parsedCommand[1] == "-c" {
					initContainer.Command = parsedCommand
				} else if len(parsedCommand) > 0 {
					initContainer.Command = []string{parsedCommand[0]}
					if len(parsedCommand) > 1 {
						initContainer.Args = parsedCommand[1:]
					}
				}
			}

			initContainers = append(initContainers, initContainer)
		}
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: rb.buildObjectMeta(),
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: rb.getLabelSelectors(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      rb.getCommonLabels(),
					Annotations: rb.buildPodAnnotations(),
				},
				Spec: corev1.PodSpec{
					ImagePullSecrets: imagePullSecrets,
					InitContainers:   initContainers,
					Containers:       []corev1.Container{container},
					Volumes:          volumes,
				},
			},
		},
	}

	return deployment, nil
}

// parseCommand handles a command string and returns it in a form suitable for Kubernetes
// It supports nested shell commands with quotes
func parseCommand(cmd string) []string {
	if cmd == "" {
		return []string{}
	}

	// Special case: detect if the command already starts with "sh -c" or "/bin/sh -c"
	// to avoid wrapping an already shell-wrapped command
	shellPrefixes := []string{"sh -c ", "/bin/sh -c ", "bash -c "}
	isAlreadyShellCommand := false

	for _, prefix := range shellPrefixes {
		if strings.HasPrefix(cmd, prefix) {
			isAlreadyShellCommand = true
			break
		}
	}

	// If it's already a shell command, parse it carefully preserving the shell command structure
	if isAlreadyShellCommand {
		// Find the first occurrence of -c and take everything after it as the shell argument
		parts := strings.SplitN(cmd, " -c ", 2)
		if len(parts) == 2 {
			shellCmd := parts[0]
			shellArg := strings.TrimSpace(parts[1])

			// If the shell argument starts and ends with quotes, remove them
			if (strings.HasPrefix(shellArg, "\"") && strings.HasSuffix(shellArg, "\"")) ||
				(strings.HasPrefix(shellArg, "'") && strings.HasSuffix(shellArg, "'")) {
				shellArg = shellArg[1 : len(shellArg)-1]
			}

			return []string{strings.TrimSpace(shellCmd), "-c", shellArg}
		}
	}

	// Check if the command contains shell operators that need a shell
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, "||") ||
		strings.Contains(cmd, "|") || strings.Contains(cmd, ">") ||
		strings.Contains(cmd, "<") {
		// Return a command that uses the shell to interpret the command string
		return []string{"/bin/sh", "-c", cmd}
	}

	// For commands without shell operators, parse while respecting quotes
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

	// Remove any remaining quote characters from the arguments
	for i, arg := range result {
		result[i] = strings.Trim(arg, "'\"")
	}

	return result
}

// buildLivenessProbe creates a liveness probe from the health check configuration
func (rb *ResourceBuilder) buildLivenessProbe() *corev1.Probe {
	if rb.service.Spec.Config.HealthCheck == nil {
		return nil
	}

	failureThreshold := int32(5)
	if rb.service.Spec.Config.HealthCheck.LivenessFailureThreshold != nil {
		failureThreshold = *rb.service.Spec.Config.HealthCheck.LivenessFailureThreshold
	}

	return rb.buildProbe(failureThreshold)
}

// buildReadinessProbe creates a readiness probe from the health check configuration
func (rb *ResourceBuilder) buildReadinessProbe() *corev1.Probe {
	if rb.service.Spec.Config.HealthCheck == nil {
		return nil
	}

	failureThreshold := int32(3)
	if rb.service.Spec.Config.HealthCheck.ReadinessFailureThreshold != nil {
		failureThreshold = *rb.service.Spec.Config.HealthCheck.ReadinessFailureThreshold
	}

	return rb.buildProbe(failureThreshold)
}

// buildStartupProbe creates a startup probe from the health check configuration
func (rb *ResourceBuilder) buildStartupProbe() *corev1.Probe {
	if rb.service.Spec.Config.HealthCheck == nil {
		return nil
	}

	failureThreshold := int32(5)
	if rb.service.Spec.Config.HealthCheck.StartupFailureThreshold != nil {
		failureThreshold = *rb.service.Spec.Config.HealthCheck.StartupFailureThreshold
	}

	return rb.buildProbe(failureThreshold)
}

// buildProbe creates a probe with the specified failure threshold
func (rb *ResourceBuilder) buildProbe(failureThreshold int32) *corev1.Probe {
	healthCheck := rb.service.Spec.Config.HealthCheck

	periodSeconds := int32(10)
	if healthCheck.PeriodSeconds != nil {
		periodSeconds = *healthCheck.PeriodSeconds
	}

	timeoutSeconds := int32(5)
	if healthCheck.TimeoutSeconds != nil {
		timeoutSeconds = *healthCheck.TimeoutSeconds
	}

	probe := &corev1.Probe{
		PeriodSeconds:    periodSeconds,
		TimeoutSeconds:   timeoutSeconds,
		FailureThreshold: failureThreshold,
	}

	switch healthCheck.Type {
	case "http":
		// If no port specified and no ports configured, we can't create a valid HTTP probe
		if healthCheck.Port == nil && len(rb.service.Spec.Config.Ports) == 0 {
			return nil
		}

		port := int32(0)
		if healthCheck.Port != nil {
			port = *healthCheck.Port
		} else if len(rb.service.Spec.Config.Ports) > 0 {
			port = rb.service.Spec.Config.Ports[0].Port
		}

		path := "/health"
		if healthCheck.Path != "" {
			path = healthCheck.Path
		}

		probe.HTTPGet = &corev1.HTTPGetAction{
			Path: path,
			Port: intstr.FromInt(int(port)),
		}

	case "exec":
		if healthCheck.Command == "" {
			return nil // No command specified, skip the probe
		}

		command := parseCommand(healthCheck.Command)
		if len(command) == 0 {
			return nil // Empty command after parsing, skip the probe
		}

		probe.Exec = &corev1.ExecAction{
			Command: command,
		}

	default:
		// Unknown type, skip the probe
		return nil
	}

	return probe
}
