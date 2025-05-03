package operator

import (
	"context"
	"fmt"
	"strings"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/discovery"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// OperatorManager handles the installation and management of required operators
type OperatorManager struct {
	client    client.Client
	scheme    *runtime.Scheme
	discovery discovery.DiscoveryInterface
}

// NewOperatorManager creates a new OperatorManager instance
func NewOperatorManager(client client.Client, scheme *runtime.Scheme, discovery discovery.DiscoveryInterface) *OperatorManager {
	return &OperatorManager{
		client:    client,
		scheme:    scheme,
		discovery: discovery,
	}
}

// EnsureOperatorInstalled checks if the required operator is installed and installs it if needed
func (m *OperatorManager) EnsureOperatorInstalled(ctx context.Context, logger logr.Logger, operatorType string, namespace string) error {
	// Check if operator is already installed
	installed, err := m.isOperatorInstalled(ctx, operatorType, namespace)
	if err != nil {
		return fmt.Errorf("failed to check if operator is installed: %w", err)
	}

	if installed {
		logger.Info("Operator already installed", "type", operatorType, "namespace", namespace)
		return nil
	}

	// Install the operator
	logger.Info("Installing operator", "type", operatorType, "namespace", namespace)
	return m.installOperator(ctx, logger, operatorType, namespace)
}

// isOperatorInstalled checks if the specified operator is installed by verifying CRD existence
func (m *OperatorManager) isOperatorInstalled(ctx context.Context, operatorType string, namespace string) (bool, error) {
	switch operatorType {
	case "mysql":
		// Check for MySQL InnoDBCluster CRD using discovery client
		resources, err := m.discovery.ServerResourcesForGroupVersion("mysql.oracle.com/v2")
		if err != nil {
			// If the error is "not found", the CRD is not installed
			if discovery.IsGroupDiscoveryFailedError(err) {
				return false, nil
			}
			// For other errors, we can't determine if the operator is installed
			return false, fmt.Errorf("failed to check MySQL operator installation: %w", err)
		}

		// Look for the InnoDBCluster resource type
		for _, r := range resources.APIResources {
			if strings.EqualFold(r.Kind, "InnoDBCluster") {
				return true, nil
			}
		}
		return false, nil

	default:
		return false, fmt.Errorf("unsupported operator type: %s", operatorType)
	}
}

// installOperator installs the specified operator using Helm
func (m *OperatorManager) installOperator(ctx context.Context, logger logr.Logger, operatorType string, namespace string) error {
	switch operatorType {
	case "mysql":
		return m.installMySQLOperator(ctx, logger, namespace)
	default:
		return fmt.Errorf("unsupported operator type: %s", operatorType)
	}
}

// installMySQLOperator installs the MySQL operator using Helm
func (m *OperatorManager) installMySQLOperator(ctx context.Context, logger logr.Logger, namespace string) error {
	// Create HelmRepository for MySQL operator
	repo := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mysql-operator",
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL: "https://mysql.github.io/mysql-operator/",
			Interval: metav1.Duration{
				Duration: 3600000000000, // 1 hour
			},
		},
	}

	if err := m.client.Create(ctx, repo); err != nil {
		return fmt.Errorf("failed to create HelmRepository: %w", err)
	}

	// Create HelmRelease for MySQL operator
	release := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mysql-operator",
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval: metav1.Duration{
				Duration: 3600000000000, // 1 hour
			},
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   "mysql-operator",
					Version: "2.2.3",
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      "HelmRepository",
						Name:      "mysql-operator",
						Namespace: namespace,
					},
				},
			},
		},
	}

	if err := m.client.Create(ctx, release); err != nil {
		return fmt.Errorf("failed to create HelmRelease: %w", err)
	}

	return nil
}
