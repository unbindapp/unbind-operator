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
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appv1 "github.com/unbindapp/unbind-operator/api/v1"
	"github.com/unbindapp/unbind-operator/internal/utils"
	networkingv1 "k8s.io/api/networking/v1"
)

// AppReconciler reconciles a App object
type AppReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=app.unbind.app,resources=apps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=app.unbind.app,resources=apps/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=app.unbind.app,resources=apps/finalizers,verbs=update

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

	// Define Deployment
	defaultReplicas := int32(1)
	if app.Spec.Replicas != nil {
		defaultReplicas = *app.Spec.Replicas
	}

	// Check if the port is specified, if not try to infer it from the image
	port := app.Spec.Port
	if port == nil {
		// Infer the port from the image
		inferredPort, err := utils.InferPortFromImage(app.Spec.Image)
		if err != nil {
			return ctrl.Result{}, err
		}
		port = ptr.To(inferredPort)
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(defaultReplicas),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": app.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "app",
						Image: app.Spec.Image,
						Ports: []corev1.ContainerPort{{
							ContainerPort: *port,
						}},
					}},
				},
			},
		},
	}

	// Set owner reference for garbage collection
	if err := ctrl.SetControllerReference(&app, deployment, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// Create or update deployment
	var existingDeployment appsv1.Deployment
	err := r.Get(ctx, client.ObjectKey{Namespace: deployment.Namespace, Name: deployment.Name}, &existingDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			// Create deployment
			log.Info("Creating Deployment", "namespace", deployment.Namespace, "name", deployment.Name)
			if err := r.Create(ctx, deployment); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	}
	// ! TODO - handle update

	// Define service
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": app.Name},
			Ports: []corev1.ServicePort{{
				Protocol:   corev1.ProtocolTCP,
				Port:       *port,
				TargetPort: intstr.FromInt32(*port),
			}},
		},
	}
	if err := ctrl.SetControllerReference(&app, service, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// 5. Create or Update the Service
	var existingService corev1.Service
	err = r.Get(ctx, client.ObjectKey{Name: service.Name, Namespace: service.Namespace}, &existingService)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating Service", "service", service.Name)
			if err := r.Create(ctx, service); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	}

	// ! TODO - handle update

	// 6. Define the desired Ingress
	// For Ingress, we use the apps’s domain from the spec.
	// Adjust the Ingress spec as needed for your environment.
	pathType := networkingv1.PathTypePrefix

	ingress := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Annotations: map[string]string{
				"kubernetes.io/ingress.class":                        "nginx",
				"cert-manager.io/cluster-issuer":                     "letsencrypt-prod",
				"nginx.ingress.kubernetes.io/eventsource":            "true",
				"nginx.ingress.kubernetes.io/add-base-url":           "true",
				"nginx.ingress.kubernetes.io/ssl-redirect":           "true",
				"nginx.ingress.kubernetes.io/websocket-services":     fmt.Sprintf("%s-service", app.Name),
				"nginx.ingress.kubernetes.io/proxy-send-timeout":     "1800",
				"nginx.ingress.kubernetes.io/proxy-read-timeout":     "21600",
				"nginx.ingress.kubernetes.io/proxy-body-size":        "10m",
				"nginx.ingress.kubernetes.io/upstream-hash-by":       "$realip_remote_addr",
				"nginx.ingress.kubernetes.io/affinity":               "cookie",
				"nginx.ingress.kubernetes.io/session-cookie-name":    fmt.Sprintf("%s-session", app.Name),
				"nginx.ingress.kubernetes.io/session-cookie-expires": "172800",
				"nginx.ingress.kubernetes.io/session-cookie-max-age": "172800",
			},
		},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{app.Spec.Domain},
					SecretName: fmt.Sprintf("%s-tls-secret", strings.ToLower(app.Name)),
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: app.Spec.Domain,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/",
									PathType: &pathType,
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: app.Name,
											Port: networkingv1.ServiceBackendPort{
												Number: *port,
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	// Set owner reference for garbage collection
	if err := ctrl.SetControllerReference(&app, ingress, r.Scheme); err != nil {
		return ctrl.Result{}, err
	}

	// 7. Create or Update the Ingress
	var existingIngress networkingv1.Ingress
	err = r.Get(ctx, client.ObjectKey{Name: ingress.Name, Namespace: ingress.Namespace}, &existingIngress)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("Creating Ingress", "ingress", ingress.Name)
			if err := r.Create(ctx, ingress); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			return ctrl.Result{}, err
		}
	}
	// ! TODO - handle update

	// 8. Reconciliation successful – nothing left to do.
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&appv1.App{}).
		Named("app").
		Complete(r)
}
