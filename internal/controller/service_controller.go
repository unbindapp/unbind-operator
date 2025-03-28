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
	"reflect"

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

const (
	serviceFinalizer = "service.unbind.unbind.app/finalizer"
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
func (r *ServiceReconciler) reconcileDeployment(ctx context.Context, rb *resourcebuilder.ResourceBuilder, service v1.Service) error {
	logger := log.FromContext(ctx)

	// Build desired deployment
	desired, err := rb.BuildDeployment()
	if err != nil {
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
func (r *ServiceReconciler) reconcileService(ctx context.Context, rb *resourcebuilder.ResourceBuilder, service v1.Service) error {
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
func (r *ServiceReconciler) reconcileIngress(ctx context.Context, rb *resourcebuilder.ResourceBuilder, service v1.Service) error {
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

// SetupWithManager sets up the controller with the Manager
func (r *ServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Service{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Owns(&networkingv1.Ingress{}).
		Complete(r)
}
