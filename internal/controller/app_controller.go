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
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/google/go-containerregistry/pkg/authn"
	appv1 "github.com/unbindapp/unbind-operator/api/v1"
	"github.com/unbindapp/unbind-operator/internal/resourcebuilder"
	"github.com/unbindapp/unbind-operator/internal/utils"
	networkingv1 "k8s.io/api/networking/v1"
)

// AppReconciler reconciles a App object
type AppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=app.unbind.cloud,resources=apps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=app.unbind.cloud,resources=apps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=app.unbind.cloud,resources=apps/finalizers,verbs=update

// RBAC for Deployments (built-in resource)
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

// RBAC for StatefulSets (built-in resource)
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete

// RBAC for Services (core API group)
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

// RBAC for PersistentVolumeClaims (PVCs) (core API group)
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete

// RBAC for Ingresses (typically in the networking API group)
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update;patch;delete

// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the App object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile

// ! TODO - add annotations or labels?
func (r *AppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var app appv1.App
	if err := r.Get(ctx, req.NamespacedName, &app); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Default replicas to 1
	defaultReplicas := int32(1)
	if app.Spec.Replicas == nil {
		app.Spec.Replicas = ptr.To(defaultReplicas)
	}

	// Check if the port is specified, if not try to infer it from the image
	port := app.Spec.Port
	if port == nil {
		// Infer the port from the image
		// Try to get secret from image pull secret
		auth, err := r.getRegistrySecret(ctx, app)
		if err != nil {
			log.Error(err, "failed to get registry secret")
		}
		inferredPort, err := utils.InferPortFromImage(app.Spec.Image, auth)
		if err != nil {
			return ctrl.Result{}, err
		}
		app.Spec.Port = ptr.To(inferredPort)
	}

	// Begin reconciling
	log.Info("Starting reconcile", "appName", req.Name, "image", app.Spec.Image, "replicas", *app.Spec.Replicas, "port", *app.Spec.Port, "domain", app.Spec.Domain)

	// Create ResourceBuilder
	rb := resourcebuilder.NewResourceBuilder(&app, r.Scheme)

	// Reconcile Deployment
	deployment, err := rb.BuildDeployment()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building deployment: %w", err)
	}
	if err := r.reconcileDeployment(ctx, deployment); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling deployment: %w", err)
	}

	// Reconcile Service
	service, err := rb.BuildService()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building service: %w", err)
	}
	if err := r.reconcileService(ctx, service); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling service: %w", err)
	}

	// Reconcile Ingress
	ingress, err := rb.BuildIngress()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("building ingress: %w", err)
	}
	if err := r.reconcileIngress(ctx, ingress); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling ingress: %w", err)
	}

	return ctrl.Result{}, nil

}

type DockerConfigJSON struct {
	Auths map[string]DockerAuth `json:"auths"`
}

type DockerAuth struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Auth     string `json:"auth"`
}

func (r *AppReconciler) getRegistrySecret(ctx context.Context, app appv1.App) (*authn.Basic, error) {
	// Get the secret using controller-runtime client
	var secret corev1.Secret
	if err := r.Get(ctx, client.ObjectKey{
		Namespace: app.Namespace,
		Name:      app.Spec.ImagePullSecret,
	}, &secret); err != nil {
		return nil, fmt.Errorf("failed to get image pull secret: %w", err)
	}

	// Parse the Docker config JSON from the secret
	var dockerConfig DockerConfigJSON
	if err := json.Unmarshal(secret.Data[".dockerconfigjson"], &dockerConfig); err != nil {
		return nil, fmt.Errorf("failed to parse docker config: %w", err)
	}

	// Find the matching auth for our registry
	registry := strings.Split(app.Spec.Image, "/")[0]
	auth, exists := dockerConfig.Auths[registry]
	if !exists {
		return nil, fmt.Errorf("no auth found for registry: %s", registry)
	}

	return &authn.Basic{
		Username: auth.Username,
		Password: auth.Password,
	}, nil
}

func (r *AppReconciler) reconcileDeployment(ctx context.Context, desired *appsv1.Deployment) error {
	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
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

func (r *AppReconciler) reconcileService(ctx context.Context, desired *corev1.Service) error {
	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	// Preserve the ClusterIP
	desired.Spec.ClusterIP = existing.Spec.ClusterIP

	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		return r.Update(ctx, &existing)
	}

	return nil
}

func (r *AppReconciler) reconcileIngress(ctx context.Context, desired *networkingv1.Ingress) error {
	var existing networkingv1.Ingress
	err := r.Get(ctx, client.ObjectKey{Namespace: desired.Namespace, Name: desired.Name}, &existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	if !reflect.DeepEqual(existing.Spec, desired.Spec) {
		existing.Spec = desired.Spec
		existing.Annotations = desired.Annotations
		return r.Update(ctx, &existing)
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1.App{}).
		Named("unbind-app-controller").
		Complete(r)
}
