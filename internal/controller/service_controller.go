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

package controller

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"reflect"
	"slices"
	"time"

	mocov1beta2 "github.com/cybozu-go/moco/api/v1beta2"
	helmv2 "github.com/fluxcd/helm-controller/api/v2"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	mdbv1 "github.com/mongodb/mongodb-kubernetes-operator/api/v1"
	v1 "github.com/unbindapp/unbind-operator/api/v1"
	"github.com/unbindapp/unbind-operator/internal/operator"
	"github.com/unbindapp/unbind-operator/internal/resourcebuilder"
	postgresv1 "github.com/zalando/postgres-operator/pkg/apis/acid.zalan.do/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	serviceFinalizer = "service.unbind.unbind.app/finalizer"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	OperatorManager *operator.OperatorManager
}

func (r *ServiceReconciler) newResourceBuilder(service *v1.Service) resourcebuilder.ResourceBuilderInterface {
	return resourcebuilder.NewResourceBuilder(service, r.Scheme)
}

// +kubebuilder:rbac:groups=unbind.unbind.app,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=unbind.unbind.app,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unbind.unbind.app,resources=services/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=acid.zalan.do,resources=postgresqls,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.toolkit.fluxcd.io,resources=helmreleases/status,verbs=get
// +kubebuilder:rbac:groups=source.toolkit.fluxcd.io,resources=helmrepositories/status,verbs=get
// +kubebuilder:rbac:groups=moco.cybozu.com,resources=mysqlclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=moco.cybozu.com,resources=backuppolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=mongodbcommunity.mongodb.com,resources=mongodbcommunity,verbs=get;list;watch;create;update;patch;delete

// Reconcile is the main reconciliation loop for the Service resource
func (r *ServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Service", "service", req.NamespacedName)

	// Fetch the Service instance
	var service v1.Service
	if err := r.Get(ctx, req.NamespacedName, &service); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Check if service is being deleted
	if !service.ObjectMeta.DeletionTimestamp.IsZero() {
		// Object is being deleted
		if controllerutil.ContainsFinalizer(&service, serviceFinalizer) {
			// Run finalization logic
			if err := r.finalizeService(ctx, &service); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to finalize service: %w", err)
			}

			// Remove finalizer once cleanup is done
			controllerutil.RemoveFinalizer(&service, serviceFinalizer)
			if err := r.Update(ctx, &service); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add finalizer if it doesn't exist
	if !controllerutil.ContainsFinalizer(&service, serviceFinalizer) {
		controllerutil.AddFinalizer(&service, serviceFinalizer)
		if err := r.Update(ctx, &service); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add finalizer: %w", err)
		}
		// Return early as the update will trigger another reconciliation
		return ctrl.Result{}, nil
	}

	// Build resource builder
	rb := r.newResourceBuilder(&service)

	// Determine if this is a database
	if service.Spec.Type == "database" {
		if err := r.reconcileDatabase(ctx, rb, service); err != nil {
			logger.Error(err, "Failed to reconcile runtime objects")
			return ctrl.Result{}, err
		}
	} else {
		// * Generic path
		// Create or update the Deployment
		if err := r.reconcileDeployment(ctx, rb, service); err != nil {
			logger.Error(err, "Failed to reconcile Deployment")
			return ctrl.Result{}, err
		}

		// Create or update the Service
		if err := r.reconcileService(ctx, rb, service); err != nil {
			logger.Error(err, "Failed to reconcile Service")
			return ctrl.Result{}, err
		}

		// Create or update the Ingress if needed
		if err := r.reconcileIngress(ctx, rb, service); err != nil {
			logger.Error(err, "Failed to reconcile Ingress")
			return ctrl.Result{}, err
		}
	}

	// Update status
	var newURLs []string
	if len(service.Spec.Config.Hosts) > 0 && service.Spec.Config.Public {
		for _, host := range service.Spec.Config.Hosts {
			newURLs = append(newURLs, fmt.Sprintf("https://%s", host.Host))
		}
	}

	// Only update status if needed
	needsStatusUpdate := false
	if service.Status.DeploymentStatus != "Ready" {
		service.Status.DeploymentStatus = "Ready"
		needsStatusUpdate = true
	}

	if !reflect.DeepEqual(service.Status.URLs, newURLs) {
		service.Status.URLs = newURLs
		needsStatusUpdate = true
	}

	if needsStatusUpdate {
		if err := r.Status().Update(ctx, &service); err != nil {
			logger.Error(err, "Failed to update Service status")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// finalizeService handles resource cleanup when the Service CR is being deleted
func (r *ServiceReconciler) finalizeService(ctx context.Context, service *v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Finalizing Service", "service", fmt.Sprintf("%s/%s", service.Namespace, service.Name))

	// Delete dependent resources
	// Since we've set ownership references, Kubernetes will automatically delete owned resources
	// But we can explicitly delete them here to ensure they're gone before finalizer is removed

	// Check for and delete Deployment
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}
	if err := r.Delete(ctx, deployment); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	// Check for and delete Service
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}
	if err := r.Delete(ctx, svc); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete service: %w", err)
	}

	// Check for and delete Ingress
	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      service.Name,
			Namespace: service.Namespace,
		},
	}
	if err := r.Delete(ctx, ingress); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete ingress: %w", err)
	}

	logger.Info("Service finalization complete")
	return nil
}

// reconcileDeployment ensures the Deployment exists and is configured correctly
func (r *ServiceReconciler) reconcileDeployment(ctx context.Context, rb resourcebuilder.ResourceBuilderInterface, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Build desired deployment
	desired, err := rb.BuildDeployment()

	// If the deployment is not needed, delete any existing
	if err == resourcebuilder.ErrDeploymentNotNeeded {
		logger.Info("Deployment not needed, deleting if exists")
		var existing appsv1.Deployment
		err = r.Get(ctx, client.ObjectKey{Namespace: service.Namespace, Name: service.Name}, &existing)

		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Deployment already deleted or doesn't exist")
				return nil
			}
			return fmt.Errorf("checking if deployment exists: %w", err)
		}

		logger.Info("Deleting deployment", "name", existing.Name)
		if err := r.Delete(ctx, &existing); err != nil {
			return fmt.Errorf("deleting Deployment: %w", err)
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to build deployment")
		return fmt.Errorf("building deployment: %w", err)
	}

	var existing appsv1.Deployment
	err = r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	// Update if needed
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, &existing)
	}

	return nil
}

// reconcileService ensures the Service exists and is configured correctly
// or is deleted if not needed
func (r *ServiceReconciler) reconcileService(ctx context.Context, rb resourcebuilder.ResourceBuilderInterface, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Check if service is needed
	desired, err := rb.BuildService()

	// If the service is not needed (no port configured), delete any existing service
	if err == resourcebuilder.ErrServiceNotNeeded {
		logger.Info("Service not needed, deleting if exists")
		existingService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Name,
				Namespace: service.Namespace,
			},
		}

		err := r.Get(ctx, types.NamespacedName{Name: existingService.Name, Namespace: existingService.Namespace}, existingService)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Service already deleted or doesn't exist")
				return nil
			}
			return fmt.Errorf("checking if service exists: %w", err)
		}

		logger.Info("Deleting service", "name", existingService.Name)
		if err := r.Delete(ctx, existingService); err != nil {
			return fmt.Errorf("deleting service: %w", err)
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to build service")
		return fmt.Errorf("building service: %w", err)
	}

	// Service is needed, get existing or create new
	var existing corev1.Service
	err = r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating service", "name", desired.Name)
			if err := controllerutil.SetControllerReference(&service, desired, r.Scheme); err != nil {
				return fmt.Errorf("setting controller reference: %w", err)
			}
			return r.Create(ctx, desired)
		}
		return fmt.Errorf("getting service: %w", err)
	}

	// Copy ClusterIP which is immutable
	desired.Spec.ClusterIP = existing.Spec.ClusterIP

	// Compare specs to determine if an update is needed
	// Only compare relevant fields to avoid unnecessary updates
	needsUpdate := false

	// Compare ports
	if !reflect.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) {
		needsUpdate = true
	}

	// Compare selector
	if !reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		needsUpdate = true
	}

	// Compare type
	if existing.Spec.Type != desired.Spec.Type {
		needsUpdate = true
	}

	// Only update if needed
	if needsUpdate {
		logger.Info("Updating service", "name", desired.Name)
		existing.Spec = desired.Spec
		if err := controllerutil.SetControllerReference(&service, &existing, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference: %w", err)
		}
		return r.Update(ctx, &existing)
	}

	logger.Info("Service already up to date")
	return nil
}

// reconcileIngress ensures the Ingress exists and is configured correctly
// or is deleted if not needed
func (r *ServiceReconciler) reconcileIngress(ctx context.Context, rb resourcebuilder.ResourceBuilderInterface, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Check if ingress is needed
	desired, err := rb.BuildIngress()

	// If the ingress is not needed (no host or not public), delete any existing ingress
	if err == resourcebuilder.ErrIngressNotNeeded {
		logger.Info("Ingress not needed, deleting if exists")
		existingIngress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Name,
				Namespace: service.Namespace,
			},
		}

		err := r.Get(ctx, types.NamespacedName{Name: existingIngress.Name, Namespace: existingIngress.Namespace}, existingIngress)
		if err != nil {
			if errors.IsNotFound(err) {
				logger.Info("Ingress already deleted or doesn't exist")
				return nil
			}
			return fmt.Errorf("checking if ingress exists: %w", err)
		}

		logger.Info("Deleting ingress", "name", existingIngress.Name)
		if err := r.Delete(ctx, existingIngress); err != nil {
			return fmt.Errorf("deleting ingress: %w", err)
		}
		return nil
	} else if err != nil {
		logger.Error(err, "Failed to build ingress")
		return fmt.Errorf("building ingress: %w", err)
	}

	// Ingress is needed, get existing or create new
	var existing networkingv1.Ingress
	err = r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Creating ingress", "name", desired.Name)
			if err := controllerutil.SetControllerReference(&service, desired, r.Scheme); err != nil {
				return fmt.Errorf("setting controller reference: %w", err)
			}
			return r.Create(ctx, desired)
		}
		return fmt.Errorf("getting ingress: %w", err)
	}

	// Update if needed
	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		logger.Info("Updating ingress", "name", desired.Name)
		existing.Spec = desired.Spec
		if err := controllerutil.SetControllerReference(&service, &existing, r.Scheme); err != nil {
			return fmt.Errorf("setting controller reference: %w", err)
		}
		return r.Update(ctx, &existing)
	}

	logger.Info("Ingress already up to date")
	return nil
}

// reconcileDatabase handles Service resources of type "database"
func (r *ServiceReconciler) reconcileDatabase(ctx context.Context, rb resourcebuilder.ResourceBuilderInterface, service v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling database", "service", fmt.Sprintf("%s/%s", service.Namespace, service.Name))

	// Get the controller's namespace
	controllerNamespace := os.Getenv("POD_NAMESPACE")
	if controllerNamespace == "" {
		return fmt.Errorf("POD_NAMESPACE environment variable not set")
	}

	// Check and install required operator if needed
	if slices.Contains([]string{"mysql", "mongodb"}, service.Spec.Config.Database.Type) {
		if err := r.OperatorManager.EnsureOperatorInstalled(ctx, logger, service.Spec.Config.Database.Type, controllerNamespace); err != nil {
			logger.Error(err, "Failed to ensure operator is installed")
			return err
		}
	}

	// Handle database-specific secret creation
	switch service.Spec.Config.Database.Type {
	case "redis":
		if service.Spec.KubernetesSecret != "" {
			if err := r.ensureRedisSecret(ctx, &service); err != nil {
				logger.Error(err, "Failed to ensure Redis secret")
				return err
			}
		}
	}

	// Get database def content from service spec
	runtimeObjects, err := rb.BuildDatabaseObjects(ctx, logger)
	if err != nil {
		return fmt.Errorf("failed to build database objects: %w", err)
	}

	// Reconcile the rendered objects
	if err := r.reconcileRuntimeObjects(ctx, runtimeObjects, service); err != nil {
		logger.Error(err, "Failed to reconcile runtime objects")
		return err
	}

	return nil
}

// ensureRedisSecret creates or updates a secret for Redis with a generated password
func (r *ServiceReconciler) ensureRedisSecret(ctx context.Context, service *v1.Service) error {
	logger := log.FromContext(ctx)
	secretName := service.Spec.KubernetesSecret

	// Check if the secret already exists
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: service.Namespace,
		Name:      secretName,
	}, secret)

	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check if Redis secret exists: %w", err)
		}

		// Secret doesn't exist, create a new one with a generated password
		password, err := generateSecurePassword(32)
		if err != nil {
			return fmt.Errorf("failed to generate Redis password: %w", err)
		}

		// Create a new secret
		newSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      secretName,
				Namespace: service.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: map[string][]byte{
				"DATABASE_PASSWORD": []byte(password),
			},
		}

		logger.Info("Creating new Redis secret", "secretName", secretName)
		if err := r.Create(ctx, newSecret); err != nil {
			return fmt.Errorf("failed to create Redis secret: %w", err)
		}
	} else {
		// Secret exists, check if it has a Redis password
		if _, ok := secret.Data["DATABASE_PASSWORD"]; !ok {
			// No password exists, generate one
			password, err := generateSecurePassword(32)
			if err != nil {
				return fmt.Errorf("failed to generate Redis password: %w", err)
			}

			// Initialize data map if needed
			if secret.Data == nil {
				secret.Data = map[string][]byte{}
			}

			// Update the secret with the password
			secret.Data["DATABASE_USERNAME"] = []byte("default")
			secret.Data["DATABASE_PASSWORD"] = []byte(password)
			secret.Data["DATABASE_URL"] = []byte(fmt.Sprintf("redis://%s:%s@%s-headless.%s:%d", "default", password, service.Name, service.Namespace, 6379))

			logger.Info("Updating existing Redis secret", "secretName", secretName)
			if err := r.Update(ctx, secret); err != nil {
				return fmt.Errorf("failed to update Redis secret: %w", err)
			}
		}
	}

	return nil
}

// generateSecurePassword generates a cryptographically secure random password
func generateSecurePassword(length int) (string, error) {
	b := make([]byte, length)
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b)[:length], nil
}

// Handle CRD-specific reconciliation
func (r *ServiceReconciler) reconcilePostgresql(ctx context.Context, postgres *postgresv1.Postgresql, owner *v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Postgresql", "name", postgres.Name, "namespace", postgres.Namespace)

	// Set controller reference
	if err := controllerutil.SetControllerReference(owner, postgres, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Check if the resource exists
	var existing postgresv1.Postgresql
	err := r.Get(ctx, client.ObjectKey{Namespace: postgres.Namespace, Name: postgres.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			logger.Info("Creating Postgresql", "name", postgres.Name)
			return r.Create(ctx, postgres)
		}
		return err
	}

	// Resource exists, check if it needs to be updated
	if !reflect.DeepEqual(existing.Spec, postgres.Spec) {
		// Update the resource
		existing.Spec = postgres.Spec
		logger.Info("Updating Postgresql", "name", postgres.Name)
		if err := r.Update(ctx, &existing); err != nil {
			return err
		}
	}

	// If we have a target secret, try to copy credentials
	if owner.Spec.KubernetesSecret != "" {
		if err := r.copyPostgresCredentials(ctx, owner); err != nil {
			logger.Error(err, "Failed to copy PostgreSQL credentials")
			return err
		}
	}

	return nil
}

// copyPostgresCredentials copies credentials from Zalando PostgreSQL secret to the target secret
func (r *ServiceReconciler) copyPostgresCredentials(ctx context.Context, service *v1.Service) error {
	logger := log.FromContext(ctx)

	// The Zalando operator creates secrets with a specific naming pattern
	zalandoSecretName := fmt.Sprintf("postgres.%s.credentials.postgresql.acid.zalan.do", service.Name)
	zalandoSecret := &corev1.Secret{}

	// Retry logic to wait for the Zalando secret to be created
	retries := 0
	var err error
	for retries < 10 {
		err = r.Get(ctx, types.NamespacedName{
			Namespace: service.Namespace,
			Name:      zalandoSecretName,
		}, zalandoSecret)

		if err == nil {
			break // Found the secret, exit the retry loop
		}

		if errors.IsNotFound(err) {
			retries++
			logger.Info("Zalando PostgreSQL secret not found yet, retrying",
				"secret", zalandoSecretName,
				"attempt", retries,
				"target", service.Spec.KubernetesSecret)
			time.Sleep(2 * time.Second)
			continue
		}

		// Some other error occurred
		return fmt.Errorf("failed to get Zalando PostgreSQL secret: %w", err)
	}

	if err != nil {
		return fmt.Errorf("failed to get Zalando PostgreSQL secret after retries: %w", err)
	}

	// Check if the target secret already exists
	targetSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Spec.KubernetesSecret,
	}, targetSecret)

	if err != nil {
		// no-op
		return nil
	}
	// Secret exists, check if it's empty or needs credentials copied
	isEmpty := len(targetSecret.Data) == 0
	hasCredentials := false

	// Check if the secret already has the credentials
	if _, ok := targetSecret.Data["DATABASE_USERNAME"]; ok {
		if _, ok := targetSecret.Data["DATABASE_PASSWORD"]; ok {
			if _, ok := targetSecret.Data["DATABASE_URL"]; ok {
				hasCredentials = true
			}
		}
	}

	if isEmpty || !hasCredentials {
		logger.Info("Target secret exists but needs credentials copied",
			"target", service.Spec.KubernetesSecret,
			"isEmpty", isEmpty)

		// Initialize map if needed
		if targetSecret.Data == nil {
			targetSecret.Data = map[string][]byte{}
		}

		// Copy credentials to the existing secret
		updateSecretData(targetSecret, zalandoSecret, service)

		if err := r.Update(ctx, targetSecret); err != nil {
			return fmt.Errorf("failed to update target secret: %w", err)
		}
	} else {
		logger.Info("Target secret already has credentials, skipping copy",
			"target", service.Spec.KubernetesSecret)
	}

	return nil
}

// updateSecretData copies the required data from Zalando secret to target secret
func updateSecretData(targetSecret *corev1.Secret, zalandoSecret *corev1.Secret, service *v1.Service) {
	// Copy the credentials (username and password)
	// Zalando PostgreSQL operator typically uses these keys
	if username, ok := zalandoSecret.Data["username"]; ok {
		targetSecret.Data["DATABASE_USERNAME"] = username
	}
	if password, ok := zalandoSecret.Data["password"]; ok {
		targetSecret.Data["DATABASE_PASSWORD"] = password
	}
	targetSecret.Data["DATABASE_URL"] = []byte(fmt.Sprintf("postgresql://%s:%s@%s.%s:%d/postgres?sslmode=disable", targetSecret.Data["DATABASE_USERNAME"], targetSecret.Data["DATABASE_PASSWORD"], service.Name, service.Namespace, 5432))
}

// reconcileHelmRelease handles HelmRelease resources
func (r *ServiceReconciler) reconcileHelmRelease(ctx context.Context, helmRelease *helmv2.HelmRelease, owner *v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling HelmRelease", "name", helmRelease.Name, "namespace", helmRelease.Namespace)

	// Set controller reference
	if err := controllerutil.SetControllerReference(owner, helmRelease, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Check if the resource exists
	var existing helmv2.HelmRelease
	err := r.Get(ctx, client.ObjectKey{Namespace: helmRelease.Namespace, Name: helmRelease.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			logger.Info("Creating HelmRelease", "name", helmRelease.Name)
			return r.Create(ctx, helmRelease)
		}
		return err
	}

	// Resource exists, check if it needs to be updated
	if !reflect.DeepEqual(existing.Spec, helmRelease.Spec) {
		// Preserve status field
		status := existing.Status.DeepCopy()

		// Update the resource
		existing.Spec = helmRelease.Spec
		existing.Status = *status

		logger.Info("Updating HelmRelease", "name", helmRelease.Name)
		return r.Update(ctx, &existing)
	}

	return nil
}

// reconcileHelmRepository handles HelmRepository resources
func (r *ServiceReconciler) reconcileHelmRepository(ctx context.Context, helmRepo *sourcev1.HelmRepository, owner *v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling HelmRepository", "name", helmRepo.Name, "namespace", helmRepo.Namespace)

	// Set controller reference
	if err := controllerutil.SetControllerReference(owner, helmRepo, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Check if the resource exists
	var existing sourcev1.HelmRepository
	err := r.Get(ctx, client.ObjectKey{Namespace: helmRepo.Namespace, Name: helmRepo.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			logger.Info("Creating HelmRepository", "name", helmRepo.Name)
			return r.Create(ctx, helmRepo)
		}
		return err
	}

	// Resource exists, check if it needs to be updated
	if !reflect.DeepEqual(existing.Spec, helmRepo.Spec) {
		// Preserve status field
		status := existing.Status.DeepCopy()

		// Update the resource
		existing.Spec = helmRepo.Spec
		existing.Status = *status

		logger.Info("Updating HelmRepository", "name", helmRepo.Name)
		return r.Update(ctx, &existing)
	}

	return nil
}

// * MySQL / Moco
// reconcileMySQLCluster handles MySqlCluster resources
func (r *ServiceReconciler) reconcileMySQLCluster(ctx context.Context, mysqlcluster *mocov1beta2.MySQLCluster, owner *v1.Service) error {
	// Always set serveridbase
	mysqlcluster.Spec.ServerIDBase = 100

	logger := log.FromContext(ctx)
	logger.Info("Reconciling MySQLCluster", "name", mysqlcluster.Name, "namespace", mysqlcluster.Namespace)

	// Set controller reference
	if err := controllerutil.SetControllerReference(owner, mysqlcluster, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Check if the resource exists
	var existing mocov1beta2.MySQLCluster
	err := r.Get(ctx, client.ObjectKey{Namespace: mysqlcluster.Namespace, Name: mysqlcluster.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			logger.Info("Creating MySQLCluster", "name", mysqlcluster.Name)
			return r.Create(ctx, mysqlcluster)
		}
		return err
	}

	// Resource exists, check if it needs to be updated
	if !reflect.DeepEqual(existing.Spec, mysqlcluster.Spec) {
		// Update the resource
		existing.Spec = mysqlcluster.Spec
		logger.Info("Updating MySQLCluster", "name", mysqlcluster.Name)
		if err := r.Update(ctx, &existing); err != nil {
			return err
		}
	}

	// If we have a target secret, try to copy credentials
	if owner.Spec.KubernetesSecret != "" {
		// Get the latest status
		if err := r.Get(ctx, client.ObjectKey{Namespace: mysqlcluster.Namespace, Name: mysqlcluster.Name}, &existing); err != nil {
			return fmt.Errorf("failed to get latest MySQL status: %w", err)
		}

		if err := r.copyMySQLCredentials(ctx, owner); err != nil {
			logger.Error(err, "Failed to copy MySQL credentials")
			return err
		}
	}

	return nil
}

// copyMySQLCredentials copies credentials from MOCO MySQL secret to the target secret
func (r *ServiceReconciler) copyMySQLCredentials(ctx context.Context, service *v1.Service) error {
	logger := log.FromContext(ctx)

	// The MOCO operator creates secrets with this naming pattern
	mocoSecretName := fmt.Sprintf("moco-%s", service.Name)
	mocoSecret := &corev1.Secret{}

	// Retry logic to wait for the MOCO secret to be created
	retries := 0
	var err error
	for retries < 10 {
		err = r.Get(ctx, types.NamespacedName{
			Namespace: service.Namespace,
			Name:      mocoSecretName,
		}, mocoSecret)

		if err == nil {
			break // Found the secret, exit the retry loop
		}

		if errors.IsNotFound(err) {
			retries++
			logger.Info("MOCO MySQL secret not found yet, retrying",
				"secret", mocoSecretName,
				"attempt", retries,
				"target", service.Spec.KubernetesSecret)
			time.Sleep(2 * time.Second)
			continue
		}

		// Some other error occurred
		return fmt.Errorf("failed to get MOCO MySQL secret: %w", err)
	}

	if err != nil {
		return fmt.Errorf("failed to get MOCO MySQL secret after retries: %w", err)
	}

	// Check if the target secret already exists
	targetSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Spec.KubernetesSecret,
	}, targetSecret)

	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check if target secret exists: %w", err)
		}

		// Create the target secret if it doesn't exist
		targetSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Spec.KubernetesSecret,
				Namespace: service.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: make(map[string][]byte),
		}

		// Copy credentials to the new secret
		updateMySQLSecretData(targetSecret, mocoSecret, service)

		if err := r.Create(ctx, targetSecret); err != nil {
			return fmt.Errorf("failed to create target secret: %w", err)
		}

		return nil
	}

	// Secret exists, check if it's empty or needs credentials copied
	isEmpty := len(targetSecret.Data) == 0
	hasCredentials := false

	// Check if the secret already has the credentials
	if _, ok := targetSecret.Data["DATABASE_USERNAME"]; ok {
		if _, ok := targetSecret.Data["DATABASE_PASSWORD"]; ok {
			if _, ok := targetSecret.Data["DATABASE_URL"]; ok {
				hasCredentials = true
			}
		}
	}

	if isEmpty || !hasCredentials {
		logger.Info("Target secret exists but needs credentials copied",
			"target", service.Spec.KubernetesSecret,
			"isEmpty", isEmpty)

		// Initialize map if needed
		if targetSecret.Data == nil {
			targetSecret.Data = map[string][]byte{}
		}

		// Copy credentials to the existing secret
		updateMySQLSecretData(targetSecret, mocoSecret, service)

		if err := r.Update(ctx, targetSecret); err != nil {
			return fmt.Errorf("failed to update target secret: %w", err)
		}
	} else {
		logger.Info("Target secret already has credentials, skipping copy",
			"target", service.Spec.KubernetesSecret)
	}

	return nil
}

// updateMySQLSecretData copies the required data from MOCO secret to target secret
func updateMySQLSecretData(targetSecret *corev1.Secret, mocoSecret *corev1.Secret, service *v1.Service) {
	// Copy the credentials (username and password)
	// MOCO MySQL operator typically uses these keys
	targetSecret.Data["DATABASE_USERNAME"] = []byte("moco-writable")
	if password, ok := mocoSecret.Data["WRITABLE_PASSWORD"]; ok {
		targetSecret.Data["DATABASE_PASSWORD"] = password
	}

	// Get the username and password, defaulting if not found
	username := string(targetSecret.Data["DATABASE_USERNAME"])
	password := string(targetSecret.Data["DATABASE_PASSWORD"])

	targetSecret.Data["DATABASE_URL"] = []byte(fmt.Sprintf("mysql://%s:%s@moco-%s.%s:%d/moco",
		username,
		password,
		service.Name,
		service.Namespace,
		3306))
}

// reconcileBackupPolicy handles BackupPolicy resources
func (r *ServiceReconciler) reconcileBackupPolicy(ctx context.Context, backupPolicy *mocov1beta2.BackupPolicy, owner *v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling BackupPolicy", "name", backupPolicy.Name, "namespace", backupPolicy.Namespace)

	// Set controller reference
	if err := controllerutil.SetControllerReference(owner, backupPolicy, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Check if the resource exists
	var existing mocov1beta2.BackupPolicy
	err := r.Get(ctx, client.ObjectKey{Namespace: backupPolicy.Namespace, Name: backupPolicy.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			logger.Info("Creating BackupPolicy", "name", backupPolicy.Name)
			return r.Create(ctx, backupPolicy)
		}
		return err
	}

	// Resource exists, check if it needs to be updated
	if !reflect.DeepEqual(existing.Spec, backupPolicy.Spec) {
		// Update the resource
		existing.Spec = backupPolicy.Spec
		logger.Info("Updating BackupPolicy", "name", backupPolicy.Name)
		return r.Update(ctx, &existing)
	}

	return nil
}

// * MongoDB
// reconcileMongoDBCommunity handles MongoDBCommunity resources
func (r *ServiceReconciler) reconcileMongoDBCommunity(ctx context.Context, mongodbCommunity *mdbv1.MongoDBCommunity, owner *v1.Service) error {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling MongoDBCommunity", "name", mongodbCommunity.Name, "namespace", mongodbCommunity.Namespace)

	// Check if the resource exists
	var existing mdbv1.MongoDBCommunity
	err := r.Get(ctx, client.ObjectKey{Namespace: mongodbCommunity.Namespace, Name: mongodbCommunity.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create the resource
			logger.Info("Creating MongoDB", "name", mongodbCommunity.Name)
			return r.Create(ctx, mongodbCommunity)
		}
		return err
	}

	// Resource exists, check if it needs to be updated
	if !reflect.DeepEqual(existing.Spec, mongodbCommunity.Spec) {
		// Update the resource
		existing.Spec = mongodbCommunity.Spec
		logger.Info("Updating MongoDB", "name", mongodbCommunity.Name)
		if err := r.Update(ctx, &existing); err != nil {
			return err
		}
	}

	// If we have a target secret, try to copy credentials
	if owner.Spec.KubernetesSecret != "" {
		// Get the latest status
		if err := r.Get(ctx, client.ObjectKey{Namespace: mongodbCommunity.Namespace, Name: mongodbCommunity.Name}, &existing); err != nil {
			return fmt.Errorf("failed to get latest MongoDB status: %w", err)
		}

		if err := r.copyMongoDBCredentials(ctx, owner); err != nil {
			logger.Error(err, "Failed to copy MongoDB credentials")
			return err
		}
	}

	return nil
}

// copyMySQLCredentials copies credentials from MOCO MySQL secret to the target secret
func (r *ServiceReconciler) copyMongoDBCredentials(ctx context.Context, service *v1.Service) error {
	logger := log.FromContext(ctx)

	// The MOCO operator creates secrets with this naming pattern
	mongoSecretName := fmt.Sprintf("%s-admin-admin", service.Name)
	mongoSecret := &corev1.Secret{}

	// Retry logic to wait for the Mongo secret to be created
	retries := 0
	var err error
	for retries < 10 {
		err = r.Get(ctx, types.NamespacedName{
			Namespace: service.Namespace,
			Name:      mongoSecretName,
		}, mongoSecret)

		if err == nil {
			break // Found the secret, exit the retry loop
		}

		if errors.IsNotFound(err) {
			retries++
			logger.Info("Mongo MySQL secret not found yet, retrying",
				"secret", mongoSecretName,
				"attempt", retries,
				"target", service.Spec.KubernetesSecret)
			time.Sleep(2 * time.Second)
			continue
		}

		// Some other error occurred
		return fmt.Errorf("failed to get MongoDB secret: %w", err)
	}

	if err != nil {
		return fmt.Errorf("failed to get MongoDB secret after retries: %w", err)
	}

	// Check if the target secret already exists
	targetSecret := &corev1.Secret{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: service.Namespace,
		Name:      service.Spec.KubernetesSecret,
	}, targetSecret)

	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check if target secret exists: %w", err)
		}

		// Create the target secret if it doesn't exist
		targetSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Spec.KubernetesSecret,
				Namespace: service.Namespace,
			},
			Type: corev1.SecretTypeOpaque,
			Data: make(map[string][]byte),
		}

		// Copy credentials to the new secret
		updateMongoDBSecretData(targetSecret, mongoSecret, service)

		if err := r.Create(ctx, targetSecret); err != nil {
			return fmt.Errorf("failed to create target secret: %w", err)
		}

		return nil
	}

	// Secret exists, check if it's empty or needs credentials copied
	isEmpty := len(targetSecret.Data) == 0
	hasCredentials := false

	// Check if the secret already has the credentials
	if _, ok := targetSecret.Data["DATABASE_USERNAME"]; ok {
		if _, ok := targetSecret.Data["DATABASE_PASSWORD"]; ok {
			if _, ok := targetSecret.Data["DATABASE_URL"]; ok {
				hasCredentials = true
			}
		}
	}

	if isEmpty || !hasCredentials {
		logger.Info("Target secret exists but needs credentials copied",
			"target", service.Spec.KubernetesSecret,
			"isEmpty", isEmpty)

		// Initialize map if needed
		if targetSecret.Data == nil {
			targetSecret.Data = map[string][]byte{}
		}

		// Copy credentials to the existing secret
		updateMongoDBSecretData(targetSecret, mongoSecret, service)

		if err := r.Update(ctx, targetSecret); err != nil {
			return fmt.Errorf("failed to update target secret: %w", err)
		}
	} else {
		logger.Info("Target secret already has credentials, skipping copy",
			"target", service.Spec.KubernetesSecret)
	}

	return nil
}

// updateMongoDBSecretData copies the required data from Mongo secret to target secret
func updateMongoDBSecretData(targetSecret *corev1.Secret, mongoSecret *corev1.Secret, service *v1.Service) {
	// Copy the credentials (username and password)
	if username, ok := mongoSecret.Data["username"]; ok {
		targetSecret.Data["DATABASE_USERNAME"] = username
	}
	if password, ok := mongoSecret.Data["password"]; ok {
		targetSecret.Data["DATABASE_PASSWORD"] = password
	}

	// Get the username and password, defaulting if not found
	username := string(targetSecret.Data["DATABASE_USERNAME"])
	password := string(targetSecret.Data["DATABASE_PASSWORD"])

	targetSecret.Data["DATABASE_URL"] = []byte(fmt.Sprintf("mongodb://%s:%s@%s.%s:%d/admin",
		username,
		password,
		service.Name,
		service.Namespace,
		27017))
}

// * Generic reconciler
// Update reconcileRuntimeObjects to handle typed CRDs
func (r *ServiceReconciler) reconcileRuntimeObjects(ctx context.Context, objects []runtime.Object, service v1.Service) error {
	logger := log.FromContext(ctx)

	for _, obj := range objects {
		// Handle typed CRDs specifically
		switch typedObj := obj.(type) {
		case *postgresv1.Postgresql:
			if err := r.reconcilePostgresql(ctx, typedObj, &service); err != nil {
				return fmt.Errorf("reconciling Postgresql: %w", err)
			}

		case *helmv2.HelmRelease:
			if err := r.reconcileHelmRelease(ctx, typedObj, &service); err != nil {
				return fmt.Errorf("reconciling HelmRelease: %w", err)
			}

		case *sourcev1.HelmRepository:
			if err := r.reconcileHelmRepository(ctx, typedObj, &service); err != nil {
				return fmt.Errorf("reconciling HelmRepository: %w", err)
			}

		case *mocov1beta2.MySQLCluster:
			if err := r.reconcileMySQLCluster(ctx, typedObj, &service); err != nil {
				return fmt.Errorf("reconciling MySQLCluster: %w", err)
			}

		case *mocov1beta2.BackupPolicy:
			if err := r.reconcileBackupPolicy(ctx, typedObj, &service); err != nil {
				return fmt.Errorf("reconciling BackupPolicy: %w", err)
			}

		case *mdbv1.MongoDBCommunity:
			if err := r.reconcileMongoDBCommunity(ctx, typedObj, &service); err != nil {
				return fmt.Errorf("reconciling MongoDBCommunity: %w", err)
			}

		default:
			// Get metadata from the object
			metaObj, err := meta.Accessor(obj)
			if err != nil {
				logger.Error(err, "Failed to get object metadata")
				return fmt.Errorf("accessing object metadata: %w", err)
			}

			// Get the object type for better logging
			gvk := obj.GetObjectKind().GroupVersionKind()
			kind := gvk.Kind
			if kind == "" {
				kind = fmt.Sprintf("%T", obj)
			}

			// Check if the object exists
			existing, err := r.getExistingObject(ctx, obj)
			if err != nil {
				if !errors.IsNotFound(err) {
					logger.Error(err, "Failed to get existing object",
						"kind", kind,
						"name", metaObj.GetName(),
						"namespace", metaObj.GetNamespace())
					return fmt.Errorf("getting existing object: %w", err)
				}

				// Object doesn't exist, create it
				logger.Info("Creating object",
					"kind", kind,
					"name", metaObj.GetName(),
					"namespace", metaObj.GetNamespace(),
					"apiVersion", gvk.GroupVersion().String())

				// Set controller reference
				if err := controllerutil.SetControllerReference(&service, metaObj, r.Scheme); err != nil {
					return fmt.Errorf("setting controller reference: %w", err)
				}

				// Convert to unstructured for better CRD support if needed
				var createObj client.Object

				if clientObj, ok := obj.(client.Object); ok {
					createObj = clientObj
				} else {
					// Convert to unstructured
					u := &unstructured.Unstructured{}
					u.SetGroupVersionKind(gvk)

					objData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
					if err != nil {
						return fmt.Errorf("converting object to unstructured: %w", err)
					}
					u.SetUnstructuredContent(objData)
					createObj = u
				}

				if err := r.Create(ctx, createObj); err != nil {
					return fmt.Errorf("creating %s %s/%s: %w",
						kind,
						metaObj.GetNamespace(),
						metaObj.GetName(),
						err)
				}
				continue
			}

			// Object exists, check if update is needed
			needsUpdate, err := r.objectNeedsUpdate(obj, existing)
			if err != nil {
				return fmt.Errorf("checking if update needed: %w", err)
			}

			if needsUpdate {
				logger.Info("Updating object",
					"kind", kind,
					"name", metaObj.GetName(),
					"namespace", metaObj.GetNamespace(),
					"apiVersion", gvk.GroupVersion().String())

				// Update the existing object with the desired spec
				if err := r.updateObject(existing, obj); err != nil {
					return fmt.Errorf("updating object spec: %w", err)
				}

				// Make sure controller reference is set
				existingMeta, err := meta.Accessor(existing)
				if err != nil {
					return fmt.Errorf("getting metadata from existing object: %w", err)
				}

				if err := controllerutil.SetControllerReference(&service, existingMeta, r.Scheme); err != nil {
					return fmt.Errorf("setting controller reference: %w", err)
				}

				// Convert to client.Object
				var updateObj client.Object

				if clientObj, ok := existing.(client.Object); ok {
					updateObj = clientObj
				} else {
					return fmt.Errorf("existing object is not a client.Object: %T", existing)
				}

				if err := r.Update(ctx, updateObj); err != nil {
					return fmt.Errorf("updating %s %s/%s: %w",
						kind,
						existingMeta.GetNamespace(),
						existingMeta.GetName(),
						err)
				}
			} else {
				logger.Info("Object already up to date",
					"kind", kind,
					"name", metaObj.GetName(),
					"namespace", metaObj.GetNamespace())
			}
		}
	}

	return nil
}

// getExistingObject gets an existing object of the same type and with the same name/namespace
func (r *ServiceReconciler) getExistingObject(ctx context.Context, obj runtime.Object) (runtime.Object, error) {
	// Get metadata for name and namespace
	metaObj, err := meta.Accessor(obj)
	if err != nil {
		return nil, fmt.Errorf("getting metadata: %w", err)
	}

	// Get the object type
	gvk := obj.GetObjectKind().GroupVersionKind()

	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(gvk)

	err = r.Get(ctx, client.ObjectKey{
		Namespace: metaObj.GetNamespace(),
		Name:      metaObj.GetName(),
	}, u)

	if err != nil {
		return nil, err
	}

	// For built-in types, convert back to the specific type for easier handling
	// but keep as unstructured for CRDs and unknown types
	switch gvk.Kind {
	case "Deployment":
		deployment := &appsv1.Deployment{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, deployment)
		if err != nil {
			return nil, fmt.Errorf("converting unstructured to Deployment: %w", err)
		}
		return deployment, nil
	case "Service":
		service := &corev1.Service{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, service)
		if err != nil {
			return nil, fmt.Errorf("converting unstructured to Service: %w", err)
		}
		return service, nil
	case "Ingress":
		ingress := &networkingv1.Ingress{}
		err = runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, ingress)
		if err != nil {
			return nil, fmt.Errorf("converting unstructured to Ingress: %w", err)
		}
		return ingress, nil
	default:
		// For CRDs or unknown types, return as unstructured
		return u, nil
	}
}

// objectNeedsUpdate determines if an object needs to be updated
func (r *ServiceReconciler) objectNeedsUpdate(desired, existing runtime.Object) (bool, error) {
	// Handle unstructured objects (CRDs and unknown types)
	if _, ok := existing.(*unstructured.Unstructured); ok {
		return r.genericObjectNeedsUpdate(desired, existing)
	}

	// Handle known types
	switch desiredTyped := desired.(type) {
	case *appsv1.Deployment:
		existingTyped, ok := existing.(*appsv1.Deployment)
		if !ok {
			return false, fmt.Errorf("existing object is not a Deployment")
		}
		return !reflect.DeepEqual(existingTyped.Spec, desiredTyped.Spec), nil

	case *corev1.Service:
		existingTyped, ok := existing.(*corev1.Service)
		if !ok {
			return false, fmt.Errorf("existing object is not a Service")
		}

		// For services, only compare relevant fields
		needsUpdate := false

		// Compare ports
		if !reflect.DeepEqual(existingTyped.Spec.Ports, desiredTyped.Spec.Ports) {
			needsUpdate = true
		}

		// Compare selector
		if !reflect.DeepEqual(existingTyped.Spec.Selector, desiredTyped.Spec.Selector) {
			needsUpdate = true
		}

		// Compare type
		if existingTyped.Spec.Type != desiredTyped.Spec.Type {
			needsUpdate = true
		}

		return needsUpdate, nil

	case *networkingv1.Ingress:
		existingTyped, ok := existing.(*networkingv1.Ingress)
		if !ok {
			return false, fmt.Errorf("existing object is not an Ingress")
		}
		return !reflect.DeepEqual(existingTyped.Spec, desiredTyped.Spec), nil

	case *corev1.ConfigMap:
		existingTyped, ok := existing.(*corev1.ConfigMap)
		if !ok {
			return false, fmt.Errorf("existing object is not a ConfigMap")
		}
		return !reflect.DeepEqual(existingTyped.Data, desiredTyped.Data) ||
			!reflect.DeepEqual(existingTyped.BinaryData, desiredTyped.BinaryData), nil

	case *corev1.Secret:
		existingTyped, ok := existing.(*corev1.Secret)
		if !ok {
			return false, fmt.Errorf("existing object is not a Secret")
		}
		return !reflect.DeepEqual(existingTyped.Data, desiredTyped.Data) ||
			!reflect.DeepEqual(existingTyped.StringData, desiredTyped.StringData) ||
			existingTyped.Type != desiredTyped.Type, nil

	default:
		// For unknown typed objects, fallback to generic comparison
		return r.genericObjectNeedsUpdate(desired, existing)
	}
}

// genericObjectNeedsUpdate provides a generic comparison for objects without type-specific logic
func (r *ServiceReconciler) genericObjectNeedsUpdate(desired, existing runtime.Object) (bool, error) {
	// Convert both objects to unstructured to allow for generic field access
	desiredUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
	if err != nil {
		return false, fmt.Errorf("converting desired object to unstructured: %w", err)
	}

	existingUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existing)
	if err != nil {
		return false, fmt.Errorf("converting existing object to unstructured: %w", err)
	}

	// Remove fields we don't want to compare
	desiredUnstructured = removeNonComparedFields(desiredUnstructured)
	existingUnstructured = removeNonComparedFields(existingUnstructured)

	// Compare the filtered objects
	return !reflect.DeepEqual(desiredUnstructured, existingUnstructured), nil
}

// removeNonComparedFields removes fields that should not be part of the comparison
func removeNonComparedFields(obj map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for k, v := range obj {
		// Skip status and resource version/generation fields
		if k == "status" {
			continue
		}

		if k == "metadata" {
			if metadata, ok := v.(map[string]interface{}); ok {
				filteredMetadata := make(map[string]interface{})
				for mk, mv := range metadata {
					// Keep only relevant metadata fields
					if mk != "resourceVersion" && mk != "generation" &&
						mk != "uid" && mk != "creationTimestamp" &&
						mk != "managedFields" && mk != "selfLink" {
						filteredMetadata[mk] = mv
					}
				}
				result[k] = filteredMetadata
			}
			continue
		}

		result[k] = v
	}
	return result
}

// updateObject updates the spec of an existing object with the desired values
func (r *ServiceReconciler) updateObject(existing, desired runtime.Object) error {
	// Handle unstructured objects (CRDs and unknown types)
	if unstructuredExisting, ok := existing.(*unstructured.Unstructured); ok {
		return r.updateUnstructuredObject(unstructuredExisting, desired)
	}

	// Handle known types
	switch desiredTyped := desired.(type) {
	case *appsv1.Deployment:
		existingTyped, ok := existing.(*appsv1.Deployment)
		if !ok {
			return fmt.Errorf("existing object is not a Deployment")
		}
		existingTyped.Spec = desiredTyped.Spec

	case *corev1.Service:
		existingTyped, ok := existing.(*corev1.Service)
		if !ok {
			return fmt.Errorf("existing object is not a Service")
		}
		// For Services, preserve the ClusterIP which is immutable
		clusterIP := existingTyped.Spec.ClusterIP
		existingTyped.Spec = desiredTyped.Spec
		existingTyped.Spec.ClusterIP = clusterIP

	case *networkingv1.Ingress:
		existingTyped, ok := existing.(*networkingv1.Ingress)
		if !ok {
			return fmt.Errorf("existing object is not an Ingress")
		}
		existingTyped.Spec = desiredTyped.Spec

	case *corev1.ConfigMap:
		existingTyped, ok := existing.(*corev1.ConfigMap)
		if !ok {
			return fmt.Errorf("existing object is not a ConfigMap")
		}
		existingTyped.Data = desiredTyped.Data
		existingTyped.BinaryData = desiredTyped.BinaryData

	case *corev1.Secret:
		existingTyped, ok := existing.(*corev1.Secret)
		if !ok {
			return fmt.Errorf("existing object is not a Secret")
		}
		existingTyped.Data = desiredTyped.Data
		existingTyped.StringData = desiredTyped.StringData
		existingTyped.Type = desiredTyped.Type

	default:
		// For unknown typed objects, fallback to generic approach
		return r.genericUpdateObject(existing, desired)
	}

	return nil
}

// updateUnstructuredObject updates an unstructured object with the desired values
func (r *ServiceReconciler) updateUnstructuredObject(existing *unstructured.Unstructured, desired runtime.Object) error {
	// Convert desired to unstructured if it's not already
	var desiredUnstructured *unstructured.Unstructured

	if u, ok := desired.(*unstructured.Unstructured); ok {
		desiredUnstructured = u
	} else {
		// Convert to unstructured
		u := &unstructured.Unstructured{}
		u.SetGroupVersionKind(desired.GetObjectKind().GroupVersionKind())

		objData, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
		if err != nil {
			return fmt.Errorf("converting desired to unstructured: %w", err)
		}
		u.SetUnstructuredContent(objData)
		desiredUnstructured = u
	}

	// Preserve existing metadata and immutable fields
	existingObj := existing.UnstructuredContent()
	desiredObj := desiredUnstructured.UnstructuredContent()

	// Preserve metadata fields that should not be updated
	existingMeta, hasExistingMeta := existingObj["metadata"].(map[string]interface{})
	desiredMeta, hasDesiredMeta := desiredObj["metadata"].(map[string]interface{})

	if hasExistingMeta && hasDesiredMeta {
		// Fields to preserve from existing metadata
		preserveFields := []string{
			"resourceVersion",
			"uid",
			"generation",
			"creationTimestamp",
			"selfLink",
			"managedFields",
		}

		for _, field := range preserveFields {
			if val, exists := existingMeta[field]; exists {
				desiredMeta[field] = val
			}
		}

		// Update metadata in desired object
		desiredObj["metadata"] = desiredMeta
	}

	// Preserve known immutable fields based on resource kind
	kind := desiredUnstructured.GetKind()

	if kind == "Service" {
		// Preserve Service ClusterIP (immutable)
		if existingSpec, hasExistingSpec := existingObj["spec"].(map[string]interface{}); hasExistingSpec {
			if desiredSpec, hasDesiredSpec := desiredObj["spec"].(map[string]interface{}); hasDesiredSpec {
				if clusterIP, exists := existingSpec["clusterIP"]; exists && clusterIP != nil {
					desiredSpec["clusterIP"] = clusterIP
				}
				desiredObj["spec"] = desiredSpec
			}
		}
	}

	// Do not update status field
	if existingStatus, exists := existingObj["status"]; exists {
		desiredObj["status"] = existingStatus
	}

	// Update existing with updated content
	existing.SetUnstructuredContent(desiredObj)

	return nil
}

// genericUpdateObject provides a generic update mechanism for objects without type-specific logic
func (r *ServiceReconciler) genericUpdateObject(existing, desired runtime.Object) error {
	// Convert both objects to unstructured
	desiredUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(desired)
	if err != nil {
		return fmt.Errorf("converting desired object to unstructured: %w", err)
	}

	existingUnstructured, err := runtime.DefaultUnstructuredConverter.ToUnstructured(existing)
	if err != nil {
		return fmt.Errorf("converting existing object to unstructured: %w", err)
	}

	// Preserve fields that shouldn't be updated
	preservedFields := preserveImmutableFields(existingUnstructured)

	// Update all fields except metadata and status
	for k, v := range desiredUnstructured {
		if k != "metadata" && k != "status" && k != "apiVersion" && k != "kind" {
			existingUnstructured[k] = v
		}
	}

	// Restore preserved fields
	for k, v := range preservedFields {
		existingUnstructured[k] = v
	}

	// Convert back to the original type
	return runtime.DefaultUnstructuredConverter.FromUnstructured(existingUnstructured, existing)
}

// preserveImmutableFields extracts fields that should be preserved during an update
func preserveImmutableFields(obj map[string]interface{}) map[string]interface{} {
	preserved := make(map[string]interface{})

	// Preserve spec.clusterIP for Service objects
	if spec, ok := obj["spec"].(map[string]interface{}); ok {
		if clusterIP, ok := spec["clusterIP"]; ok {
			if preserved["spec"] == nil {
				preserved["spec"] = make(map[string]interface{})
			}
			preserved["spec"].(map[string]interface{})["clusterIP"] = clusterIP
		}
	}

	return preserved
}

// SetupWithManager sets up the controller with the Manager
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
