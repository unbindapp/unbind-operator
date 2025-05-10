package operator

import (
	"context"
	"fmt"
	"strings"

	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	"github.com/go-logr/logr"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
		// Check for MOCO MySQLCluster CRD using discovery client
		resources, err := m.discovery.ServerResourcesForGroupVersion("moco.cybozu.com/v1beta2")
		if err != nil {
			// If the error is "not found", the CRD is not installed
			if discovery.IsGroupDiscoveryFailedError(err) {
				return false, nil
			}
			if errors.IsNotFound(err) {
				return false, nil
			}
			// For other errors, we can't determine if the operator is installed
			return false, fmt.Errorf("failed to check MOCO operator installation: %w", err)
		}

		// Look for the MySQLCluster resource type
		for _, r := range resources.APIResources {
			if strings.EqualFold(r.Kind, "MySQLCluster") {
				return true, nil
			}
		}
		return false, nil

	case "clickhouse":
		// Check for ClickHouseInstallation CRD using discovery client
		resources, err := m.discovery.ServerResourcesForGroupVersion("clickhouse.altinity.com/v1")
		if err != nil {
			// If the error is "not found", the CRD is not installed
			if discovery.IsGroupDiscoveryFailedError(err) {
				return false, nil
			}
			if errors.IsNotFound(err) {
				return false, nil
			}
			// For other errors, we can't determine if the operator is installed
			return false, fmt.Errorf("failed to check ClickHouse operator installation: %w", err)
		}

		// Look for the ClickHouseInstallation resource type
		for _, r := range resources.APIResources {
			if strings.EqualFold(r.Kind, "ClickHouseInstallation") {
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
	case "clickhouse":
		return m.installClickHouseOperator(ctx, logger, namespace)
	default:
		return fmt.Errorf("unsupported operator type: %s", operatorType)
	}
}

// installMySQLOperator installs the MySQL operator using Helm
func (m *OperatorManager) installMySQLOperator(ctx context.Context, logger logr.Logger, namespace string) error {
	// Create HelmRepository for MOCO operator
	repo := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moco-operator",
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL: "https://cybozu-go.github.io/moco/",
			Interval: metav1.Duration{
				Duration: 3600000000000, // 1 hour
			},
		},
	}

	if err := m.client.Create(ctx, repo); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create HelmRepository: %w", err)
		}
		logger.Info("HelmRepository already exists", "name", repo.Name, "namespace", namespace)
	} else {
		logger.Info("Created HelmRepository", "name", repo.Name, "namespace", namespace)
	}

	// Create HelmRelease for MOCO operator
	release := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moco-operator",
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval: metav1.Duration{
				Duration: 3600000000000, // 1 hour
			},
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   "moco",
					Version: "0.16.0",
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      "HelmRepository",
						Name:      "moco-operator",
						Namespace: namespace,
					},
				},
			},
			Values: &apiextensionsv1.JSON{
				Raw: []byte(`{"replicaCount": 1}`),
			},
		},
	}

	if err := m.client.Create(ctx, release); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create HelmRelease: %w", err)
		}
		logger.Info("HelmRelease already exists", "name", release.Name, "namespace", namespace)
	} else {
		logger.Info("Created HelmRelease", "name", release.Name, "namespace", namespace)
	}

	return nil
}

// installClickHouseOperator installs the ClickHouse operator using Helm
func (m *OperatorManager) installClickHouseOperator(ctx context.Context, logger logr.Logger, namespace string) error {
	// Create HelmRepository for ClickHouse operator
	repo := &sourcev1.HelmRepository{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clickhouse-operator",
			Namespace: namespace,
		},
		Spec: sourcev1.HelmRepositorySpec{
			URL: "https://altinity.github.io/clickhouse-operator/",
			Interval: metav1.Duration{
				Duration: 3600000000000, // 1 hour
			},
		},
	}

	if err := m.client.Create(ctx, repo); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create HelmRepository: %w", err)
		}
		logger.Info("HelmRepository already exists", "name", repo.Name, "namespace", namespace)
	} else {
		logger.Info("Created HelmRepository", "name", repo.Name, "namespace", namespace)
	}

	// Create HelmRelease for ClickHouse operator
	release := &helmv2.HelmRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clickhouse-operator",
			Namespace: namespace,
		},
		Spec: helmv2.HelmReleaseSpec{
			Interval: metav1.Duration{
				Duration: 3600000000000, // 1 hour
			},
			Chart: &helmv2.HelmChartTemplate{
				Spec: helmv2.HelmChartTemplateSpec{
					Chart:   "clickhouse-operator",
					Version: "0.24.5",
					SourceRef: helmv2.CrossNamespaceObjectReference{
						Kind:      "HelmRepository",
						Name:      "clickhouse-operator",
						Namespace: namespace,
					},
				},
			},
			Values: &apiextensionsv1.JSON{
				Raw: []byte(`{
					"operator": {
						"image": {
							"repository": "altinity/clickhouse-operator",
							"pullPolicy": "IfNotPresent"
						},
						"resources": {}
					},
					"metrics": {
						"enabled": false
					},
					"serviceAccount": {
						"create": true
					},
					"rbac": {
						"create": true
					},
					"secret": {
						"create": true,
						"username": "clickhouse_operator",
						"password": "clickhouse_operator_password"
					}
				}`),
			},
		},
	}

	if err := m.client.Create(ctx, release); err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create HelmRelease: %w", err)
		}
		logger.Info("HelmRelease already exists", "name", release.Name, "namespace", namespace)
	} else {
		logger.Info("Created HelmRelease", "name", release.Name, "namespace", namespace)
	}

	return nil
}
