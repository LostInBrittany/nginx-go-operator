/*
Copyright 2022.

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

package controllers

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	tutorialsv1 "github.com/LostInBrittany/nginx-go-operator/api/v1"
)

// OvhNginxReconciler reconciles a OvhNginx object
type OvhNginxReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=tutorials.ovhcloud.com,resources={ovhnginxes,secrets,serviceaccounts,services},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=tutorials.ovhcloud.com,resources=ovhnginxes/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=tutorials.ovhcloud.com,resources=ovhnginxes/finalizers,verbs=update
// Custom RBAC to allow the operator to interact with mandatory resources
//+kubebuilder:rbac:groups="",resources={secrets,serviceaccounts,services},verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete

func (r *OvhNginxReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	ovhNginx := &tutorialsv1.OvhNginx{}
	existingNginxDeployment := &appsv1.Deployment{}
	existingService := &corev1.Service{}

	log.Info("⚡️ Event received! ⚡️")
	log.Info("Request: ", "req", req)

	// CR deleted : check if  the Deployment and the Service must be deleted
	err := r.Get(ctx, req.NamespacedName, ovhNginx)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("OvhNginx resource not found, check if a deployment must be deleted.")

			// Delete Deployment
			err = r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, existingNginxDeployment)
			if err != nil {
				if errors.IsNotFound(err) {
					log.Info("Nothing to do, no deployment found.")
					return ctrl.Result{}, nil
				} else {
					log.Error(err, "❌ Failed to get Deployment")
					return ctrl.Result{}, err
				}
			} else {
				log.Info("☠️ Deployment exists: delete it. ☠️")
				r.Delete(ctx, existingNginxDeployment)
			}

			// Delete Service
			err = r.Get(ctx, types.NamespacedName{Name: req.Name, Namespace: req.Namespace}, existingService)
			if err != nil {
				if errors.IsNotFound(err) {
					log.Info("Nothing to do, no service found.")
					return ctrl.Result{}, nil
				} else {
					log.Error(err, "❌ Failed to get Service")
					return ctrl.Result{}, err
				}
			} else {
				log.Info("☠️ Service exists: delete it. ☠️")
				r.Delete(ctx, existingService)
				return ctrl.Result{}, nil
			}
		}
	} else {
		log.Info("ℹ️  CR state ℹ️", "ovhNginx.Name", ovhNginx.Name, " ovhNginx.Namespace", ovhNginx.Namespace, "ovhNginx.Spec.ReplicaCount", ovhNginx.Spec.ReplicaCount, "ovhNginx.Spec.Port", ovhNginx.Spec.Port)

		// Check if the deployment already exists, if not: create a new one.
		err = r.Get(ctx, types.NamespacedName{Name: ovhNginx.Name, Namespace: ovhNginx.Namespace}, existingNginxDeployment)
		if err != nil && errors.IsNotFound(err) {
			// Define a new deployment
			newNginxDeployment := r.createDeployment(ovhNginx)
			log.Info("✨ Creating a new Deployment", "Deployment.Namespace", newNginxDeployment.Namespace, "Deployment.Name", newNginxDeployment.Name)

			err = r.Create(ctx, newNginxDeployment)
			if err != nil {
				log.Error(err, "❌ Failed to create new Deployment", "Deployment.Namespace", newNginxDeployment.Namespace, "Deployment.Name", newNginxDeployment.Name)
				return ctrl.Result{}, err
			}
		} else if err == nil {
			// Deployment exists, check if the Deployment must be updated
			var replicaCount int32 = ovhNginx.Spec.ReplicaCount
			if *existingNginxDeployment.Spec.Replicas != replicaCount {
				log.Info("🔁 Number of replicas changes, update the deployment! 🔁")
				existingNginxDeployment.Spec.Replicas = &replicaCount
				err = r.Update(ctx, existingNginxDeployment)
				if err != nil {
					log.Error(err, "❌ Failed to update Deployment", "Deployment.Namespace", existingNginxDeployment.Namespace, "Deployment.Name", existingNginxDeployment.Name)
					return ctrl.Result{}, err
				}
			}
		} else if err != nil {
			log.Error(err, "Failed to get Deployment")
			return ctrl.Result{}, err
		}

		// Check if the service already exists, if not: create a new one
		err = r.Get(ctx, types.NamespacedName{Name: ovhNginx.Name, Namespace: ovhNginx.Namespace}, existingService)
		if err != nil && errors.IsNotFound(err) {
			// Create the Service
			newService := r.createService(ovhNginx)
			log.Info("✨ Creating a new Service", "Service.Namespace", newService.Namespace, "Service.Name", newService.Name)
			err = r.Create(ctx, newService)
			if err != nil {
				log.Error(err, "❌ Failed to create new Service", "Service.Namespace", newService.Namespace, "Service.Name", newService.Name)
				return ctrl.Result{}, err
			}
		} else if err == nil {
			// Service exists, check if the port have to be updated.
			var port int32 = ovhNginx.Spec.Port
			if existingService.Spec.Ports[0].Port != port {
				log.Info("🔁 Port number changes, update the service! 🔁")
				existingService.Spec.Ports[0].Port = port
				err = r.Update(ctx, existingService)
				if err != nil {
					log.Error(err, "❌ Failed to update Service", "Service.Namespace", existingService.Namespace, "Service.Name", existingService.Name)
					return ctrl.Result{}, err
				}
			}
		} else if err != nil {
			log.Error(err, "Failed to get Service")
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// Create a Deployment for the Nginx server.
func (r *OvhNginxReconciler) createDeployment(ovhNginxCR *tutorialsv1.OvhNginx) *appsv1.Deployment {
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ovhNginxCR.Name,
			Namespace: ovhNginxCR.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &ovhNginxCR.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "ovh-nginx-server"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": "ovh-nginx-server"},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: "ovhplatform/hello:1.0",
						Name:  "ovh-nginx",
						Ports: []corev1.ContainerPort{{
							ContainerPort: 80,
							Name:          "http",
							Protocol:      "TCP",
						}},
					}},
				},
			},
		},
	}
	return deployment
}

// Create a Service for the Nginx server.
func (r *OvhNginxReconciler) createService(ovhNginxCR *tutorialsv1.OvhNginx) *corev1.Service {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ovhNginxCR.Name,
			Namespace: ovhNginxCR.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "ovh-nginx-server",
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   corev1.ProtocolTCP,
					Port:       ovhNginxCR.Spec.Port,
					TargetPort: intstr.FromInt(80),
				},
			},
			Type: corev1.ServiceTypeLoadBalancer,
		},
	}

	return service
}

// SetupWithManager sets up the controller with the Manager.
func (r *OvhNginxReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&tutorialsv1.OvhNginx{}).
		Watches(&source.Kind{Type: &corev1.Service{}}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.Funcs{
			// Check only delete events for a service
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return false
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return true
			},
		})).
		Complete(r)
}
