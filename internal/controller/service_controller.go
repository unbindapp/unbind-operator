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
	"fmt"

	v1 "github.com/unbindapp/unbind-operator/api/v1"
	"github.com/unbindapp/unbind-operator/internal/resourcebuilder"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ServiceReconciler reconciles a Service object
type ServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=unbind.unbind.app,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=unbind.unbind.app,resources=services/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=unbind.unbind.app,resources=services/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch

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
		// The object is being deleted
		return ctrl.Result{}, nil
	}

	// Build resource builder
	rb := resourcebuilder.NewResourceBuilder(&service, r.Scheme)

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

	if service.Spec.Config.Host != nil && *service.Spec.Config.Host != "" && service.Spec.Config.Public {
		// Update status with URL
		service.Status.URL = fmt.Sprintf("https://%s", *service.Spec.Config.Host)
	} else {
		// Reset URL in status if no host or not public
		service.Status.URL = ""
	}

	// Update service status
	service.Status.DeploymentStatus = "Ready"
	if err := r.Status().Update(ctx, &service); err != nil {
		logger.Error(err, "Failed to update Service status")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileDeployment ensures the Deployment exists and is configured correctly
func (r *ServiceReconciler) reconcileDeployment(ctx context.Context, rb *resourcebuilder.ResourceBuilder, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Build desired deployment
	desired, err := rb.BuildDeployment()
	if err != nil {
		return fmt.Errorf("building deployment: %w", err)
	}

	// Set controller reference before proceeding
	if err := controllerutil.SetControllerReference(&service, desired, r.Scheme); err != nil {
		return fmt.Errorf("setting controller reference: %w", err)
	}

	// Create or update using the controller-runtime helper
	existing := &appsv1.Deployment{}
	existing.Name = desired.Name
	existing.Namespace = desired.Namespace

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, existing, func() error {
		// Copy important metadata but preserve some fields
		existing.Labels = desired.Labels
		existing.Annotations = desired.Annotations

		// Update spec - this is what ensures image changes are applied
		existing.Spec = desired.Spec

		return nil
	})

	if err != nil {
		return fmt.Errorf("creating/updating deployment: %w", err)
	}

	logger.Info("Deployment reconciled", "operation", op, "name", desired.Name)
	return nil
}

// reconcileService ensures the Service exists and is configured correctly
// or is deleted if not needed
func (r *ServiceReconciler) reconcileService(ctx context.Context, rb *resourcebuilder.ResourceBuilder, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Check if service is needed
	desired, err := rb.BuildService()

	// If the service is not needed (no port configured), delete any existing service
	if err == resourcebuilder.ErrServiceNotNeeded {
		logger.Info("Service not needed, deleting if exists")

		// Define the Service object to check for existence
		existingService := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Name,
				Namespace: service.Namespace,
			},
		}

		// Check if the service exists
		err := r.Get(ctx, types.NamespacedName{Name: existingService.Name, Namespace: existingService.Namespace}, existingService)
		if err != nil {
			if errors.IsNotFound(err) {
				// Already deleted or doesn't exist, nothing to do
				logger.Info("Service already deleted or doesn't exist")
				return nil
			}
			// Error getting service
			return fmt.Errorf("checking if service exists: %w", err)
		}

		// Service exists and needs to be deleted
		logger.Info("Deleting service", "name", existingService.Name)
		if err := r.Delete(ctx, existingService); err != nil {
			return fmt.Errorf("deleting service: %w", err)
		}

		return nil
	} else if err != nil {
		// Some other error occurred
		return fmt.Errorf("building service: %w", err)
	}

	// Service is needed, proceed with create or update
	logger.Info("Service needed, creating or updating")

	// Create or update the Service
	op, err := ctrl.CreateOrUpdate(ctx, r.Client, desired, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(&service, desired, r.Scheme); err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("creating or updating service: %w", err)
	}

	logger.Info("Service reconciled", "operation", op)
	return nil
}

// reconcileIngress ensures the Ingress exists and is configured correctly
// or is deleted if not needed
func (r *ServiceReconciler) reconcileIngress(ctx context.Context, rb *resourcebuilder.ResourceBuilder, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Check if ingress is needed
	desired, err := rb.BuildIngress()

	// If the ingress is not needed (no host or not public), delete any existing ingress
	if err == resourcebuilder.ErrIngressNotNeeded {
		logger.Info("Ingress not needed, deleting if exists")

		// Define the Ingress object to check for existence
		existingIngress := &networkingv1.Ingress{
			ObjectMeta: metav1.ObjectMeta{
				Name:      service.Name,
				Namespace: service.Namespace,
			},
		}

		// Check if the ingress exists
		err := r.Get(ctx, types.NamespacedName{Name: existingIngress.Name, Namespace: existingIngress.Namespace}, existingIngress)
		if err != nil {
			if errors.IsNotFound(err) {
				// Already deleted or doesn't exist, nothing to do
				logger.Info("Ingress already deleted or doesn't exist")
				return nil
			}
			// Error getting ingress
			return fmt.Errorf("checking if ingress exists: %w", err)
		}

		// Ingress exists and needs to be deleted
		logger.Info("Deleting ingress", "name", existingIngress.Name)
		if err := r.Delete(ctx, existingIngress); err != nil {
			return fmt.Errorf("deleting ingress: %w", err)
		}

		return nil
	} else if err != nil {
		// Some other error occurred
		return fmt.Errorf("building ingress: %w", err)
	}

	// Ingress is needed, proceed with create or update
	logger.Info("Ingress needed, creating or updating")

	// Create or update the Ingress
	op, err := ctrl.CreateOrUpdate(ctx, r.Client, desired, func() error {
		// Set controller reference
		if err := controllerutil.SetControllerReference(&service, desired, r.Scheme); err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("creating or updating ingress: %w", err)
	}

	logger.Info("Ingress reconciled", "operation", op)
	return nil
}

// getCommonLabels returns labels that should be applied to all resources
func getCommonLabels(service v1.Service) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       service.Name,
		"app.kubernetes.io/instance":   service.Name,
		"app.kubernetes.io/managed-by": "unbind-operator",
		"unbind-team":                  service.Spec.TeamRef,
		"unbind-project":               service.Spec.ProjectRef,
		"unbind-service":               service.Name,
		"unbind-environment":           service.Spec.EnvironmentID,
	}
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
