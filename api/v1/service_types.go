/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ServiceSpec defines the desired state of the Service
type ServiceSpec struct {
	// Name of the service
	Name string `json:"name"`

	// DisplayName is a human-friendly name for the service
	DisplayName string `json:"displayName,omitempty"`

	// Description of the service
	Description string `json:"description,omitempty"`

	// Type of service - git or dockerfile or template
	Type string `json:"type"`

	// Builder to use - railpack or docker or template
	Builder string `json:"builder"`

	// Provider (e.g. Go, Python, Node, Deno)
	Provider string `json:"provider,omitempty"`

	// Framework used (e.g. Django, Next, Express, Gin)
	Framework string `json:"framework,omitempty"`

	// GitHubInstallationID for GitHub integration
	GitHubInstallationID *int64 `json:"githubInstallationId,omitempty"`

	// GitRepository name
	GitRepository string `json:"gitRepository,omitempty"`

	// KubernetesSecret name for this service
	KubernetesSecret string `json:"kubernetesSecret"`

	// Configuration for the service
	Config ServiceConfigSpec `json:"config"`

	// Additional environment variables to attach
	EnvVars []corev1.EnvVar `json:"envVars,omitempty"`

	// Registry secrets to pull images from
	ImagePullSecrets []string `json:"imagePullSecrets,omitempty"`

	// DeploymentRef is a reference to the deployment this service is based on
	DeploymentRef string `json:"deploymentRef,omitempty"`

	// ServiceRef is a reference to the service this service is based on
	ServiceRef string `json:"serviceRef,omitempty"`

	// TeamRef is a reference to the team that owns this service
	TeamRef string `json:"teamRef"`

	// ProjectRef is a reference to the project this service belongs to
	ProjectRef string `json:"projectRef"`

	// EnvironmentRef references the environment this service belongs to
	EnvironmentRef string `json:"environmentRef"`

	// PodSecurityContext defines the security context for the pod
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`
}

// ServiceConfigSpec defines configuration for a service
type ServiceConfigSpec struct {
	// GitBranch to build from
	GitBranch string `json:"gitBranch,omitempty"`

	// Hosts is the external domain(s) and paths for the service
	Hosts []HostSpec `json:"hosts,omitempty"`

	// Ports are the container ports to expose
	Ports []PortSpec `json:"ports,omitempty"`

	// Replicas is the number of replicas for the service
	Replicas *int32 `json:"replicas,omitempty"`

	// RunCommand is a custom run command
	RunCommand *string `json:"runCommand,omitempty"`

	// Public indicates whether the service is publicly accessible
	Public bool `json:"public,omitempty"`

	// Image is a custom Docker image if not building from git
	Image string `json:"image"`

	// Databases are custom resources to create
	Database DatabaseSpec `json:"database"`

	// Volumes are mounted inside of the container at specified paths
	Volumes []VolumeSpec `json:"volumes,omitempty"`
}

// ServiceStatus defines the observed state of Service
type ServiceStatus struct {
	// Conditions represent the latest available observations of an object's state
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DeploymentStatus represents the status of the deployment
	DeploymentStatus string `json:"deploymentStatus,omitempty"`

	// URLs is the external URLs where the service is accessible
	URLs []string `json:"urls,omitempty"`

	// LastDeployedAt is the time when the service was last deployed
	LastDeployedAt *metav1.Time `json:"lastDeployedAt,omitempty"`
}

type HostSpec struct {
	// Host is the external domain for the service
	Host string `json:"host"`
	Path string `json:"path"`
	Port *int32 `json:"port,omitempty" required:"false"`
}

type VolumeSpec struct {
	Name      string `json:"name"`      // PVC name
	MountPath string `json:"mountPath"` // Path to mount the volume
}

type PortSpec struct {
	NodePort *int32 `json:"nodePort,omitempty" required:"false"` // NodePort will create a NodePort service
	// Port is the container port to expose
	Port     int32            `json:"port"`
	Protocol *corev1.Protocol `json:"protocol,omitempty" required:"false"`
}

type DatabaseSpec struct {
	// Type of the database
	Type string `json:"type"`
	// DatabaseSpecVersion is a reference to the version of the database spec
	DatabaseSpecVersion string               `json:"databaseSpecVersion"`
	Config              runtime.RawExtension `json:"config,omitempty"`
	// S3Config for backupps
	S3BackupConfig *S3ConfigSpec `json:"s3BackupConfig,omitempty"`
	// ! TODO - restore options
}

type S3ConfigSpec struct {
	Bucket   string `json:"bucket"`
	Endpoint string `json:"endpoint"`
	Region   string `json:"region"`
	// secret name containing the credentials
	SecretName           string `json:"secretName"`
	BackupSchedule       string `json:"backupSchedule"`
	BackupRetentionCount int    `json:"backupRetentionCount"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Team",type=string,JSONPath=`.spec.teamRef`
// +kubebuilder:printcolumn:name="Project",type=string,JSONPath=`.spec.projectRef`
// +kubebuilder:printcolumn:name="Environment",type=string,JSONPath=`.spec.environmentRef`
// +kubebuilder:printcolumn:name="Type",type=string,JSONPath=`.spec.type`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.deploymentStatus`

// Service is the Schema for the services API.
type Service struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ServiceSpec   `json:"spec,omitempty"`
	Status ServiceStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ServiceList contains a list of Service.
type ServiceList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Service `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Service{}, &ServiceList{})
}
