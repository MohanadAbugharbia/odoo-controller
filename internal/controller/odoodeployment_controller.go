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
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/log"

	odoov1 "github.com/MohanadAbugharbia/odoo-controller/api/v1"
)

// OdooDeploymentReconciler reconciles a OdooDeployment object
type OdooDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func updateStatus(odooDeploy *odoov1.OdooDeployment, statusType string, statusReason string, statusMessage string, condition metav1.ConditionStatus) {
	meta.SetStatusCondition(&odooDeploy.Status.Conditions, metav1.Condition{
		Type:               statusType,
		Status:             condition,
		Reason:             statusReason,
		LastTransitionTime: metav1.NewTime(time.Now()),
		Message:            statusMessage,
	})
}

// +kubebuilder:rbac:groups=odoo.abugharbia.com,resources=odoodeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=odoo.abugharbia.com,resources=odoodeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=odoo.abugharbia.com,resources=odoodeployments/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the OdooDeployment object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *OdooDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Reconciling OdooDeployment")

	// Fetch the OdooDeployment instance
	odooDeployment := &odoov1.OdooDeployment{}
	err := r.Get(ctx, req.NamespacedName, odooDeployment)
	if err != nil && errors.IsNotFound(err) {
		logger.Info("OdooDeployment resource object not found.")
		return ctrl.Result{}, nil
	} else if err != nil {
		logger.Error(err, "Failed to get OdooDeployment")
		updateStatus(odooDeployment, "OperatorDegraded", "FailedToGetOdooDeployment", fmt.Sprintf("Failed to get OdooDeployment: %v", err), metav1.ConditionFalse)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	// Check if odoo admin password secret already exists, if not create a new one
	adminSecret := corev1.Secret{}
	adminSecretNamespacedName := odooDeployment.CreateOdooAdminPasswordSecretNamespacedName()
	createAdminSecret := false
	err = r.Get(ctx, adminSecretNamespacedName, &adminSecret)
	if err != nil && errors.IsNotFound(err) {
		createAdminSecret = true
		adminSecret, err = odooDeployment.Spec.Config.GetOdooAdminPasswordSecretTemplate(
			adminSecretNamespacedName.Name,
			adminSecretNamespacedName.Namespace,
		)
		if err != nil {
			logger.Error(err, fmt.Sprintf("error creating %s secret.", adminSecretNamespacedName.Name))
			updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonOdooAdminSecretCreationFailed, fmt.Sprintf("error creating %s secret: %v", adminSecretNamespacedName.Name, err), metav1.ConditionFalse)
			return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
		}
		adminSecret.Name = adminSecretNamespacedName.Name
		adminSecret.Namespace = adminSecretNamespacedName.Namespace
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s secret.", adminSecretNamespacedName.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonOdooAdminSecretNotAvailable, fmt.Sprintf("error creating %s secret: %v", adminSecretNamespacedName.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	ctrl.SetControllerReference(odooDeployment, &adminSecret, r.Scheme)

	if createAdminSecret {
		logger.Info(fmt.Sprintf("Creating a new secret for %s", adminSecretNamespacedName.Name))
		err = r.Create(ctx, &adminSecret)
	} else {
		err = r.Update(ctx, &adminSecret)
	}
	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating or updating %s secret.", adminSecret.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonOdooAdminSecretCreationFailed, fmt.Sprintf("error creating or updating %s secret: %v", adminSecret.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	odooDeployment.Status.OdooAdminSecretName = adminSecret.Name
	r.Status().Update(ctx, odooDeployment)

	// Check if odoo config secret already exists, if not create a new one
	secret := corev1.Secret{}
	secretNamespacedName := odooDeployment.CreateOdooConfigSecretNamespacedName()
	createSecret := false
	err = r.Get(ctx, secretNamespacedName, &secret)
	if err != nil && errors.IsNotFound(err) {
		createSecret = true
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s secret.", req.Name))
		updateStatus(odooDeployment, "OperatorSucceeded", odoov1.ReasonOdooConfigSecretNotAvailable, fmt.Sprintf("error creating %s secret: %v", req.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	adminPassword, ok := adminSecret.Data["password"]
	if !ok {
		logger.Error(err, fmt.Sprintf("error getting admin password for %s", req.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonOdooAdminPasswordFailed, fmt.Sprintf("error getting admin password for %s from %s: %v", req.Name, adminSecret.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	secret, err = odooDeployment.CreateOdooConfigSecretObj(r.Client, ctx, string(adminPassword))
	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s secret.", secretNamespacedName.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonOdooConfigSecretCreationFailed, fmt.Sprintf("error creating %s secret: %v", secretNamespacedName.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	secret.Name = secretNamespacedName.Name
	secret.Namespace = secretNamespacedName.Namespace

	ctrl.SetControllerReference(odooDeployment, &secret, r.Scheme)

	if createSecret {
		logger.Info(fmt.Sprintf("Creating a new secret for %s", secretNamespacedName.Name))
		err = r.Create(ctx, &secret)
	} else {
		err = r.Update(ctx, &secret)
	}

	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating or updating %s secret.", secret.Name))
		updateStatus(odooDeployment, "OperatorSucceeded", odoov1.ReasonOdooConfigSecretCreationFailed, fmt.Sprintf("error creating or updating %s secret: %v", secret.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	odooDeployment.Status.OdooConfigSecretName = secret.Name

	r.Status().Update(ctx, odooDeployment)

	// Check if the PVC already exists, if not create a new one
	pvc := corev1.PersistentVolumeClaim{}
	createPvc := false
	err = r.Get(ctx, req.NamespacedName, &pvc)
	if err != nil && errors.IsNotFound(err) {
		// Create a new PVC for the OdooDeployment if it does not exist
		createPvc = true
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s pvc.", req.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonPvcNotAvailable, fmt.Sprintf("error creating %s pvc: %v", req.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	// Update the status of the OdooDeployment

	pvc = odooDeployment.GetPvcTemplate()
	ctrl.SetControllerReference(odooDeployment, &pvc, r.Scheme)

	// Here we do not try to update the PVC, because the spec field is immutable
	if createPvc {
		logger.Info(fmt.Sprintf("Creating a new PVC for %s", req.Name))
		err = r.Create(ctx, &pvc)
	}
	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating or updating %s pvc.", pvc.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonPvcCreationFailed, fmt.Sprintf("error creating or updating %s pvc: %v", req.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	odooDeployment.Status.OdooDataPvcName = pvc.Name
	r.Status().Update(ctx, odooDeployment)

	// Check if there is a current init job running
	if odooDeployment.Status.CurrentInitJob.Name != "" && odooDeployment.Status.CurrentInitJob.Namespace != "" {
		// Get the Job details
		logger.Info("Current InitJob defined, checking status")
		// Get the status of the current init job
		currentInitJob := &batchv1.Job{}
		err = r.Get(ctx, types.NamespacedName{
			Name:      odooDeployment.Status.CurrentInitJob.Name,
			Namespace: odooDeployment.Status.CurrentInitJob.Namespace,
		}, currentInitJob)
		if err != nil && errors.IsNotFound(err) {
			logger.Info("Current InitJob not found")
		} else if err != nil {
			logger.Error(err, "Failed to get current InitJob")
			updateStatus(odooDeployment, "OperatorDegraded", "FailedToGetInitJob", fmt.Sprintf("Failed to get current InitJob: %v", err), metav1.ConditionFalse)
			return ctrl.Result{RequeueAfter: 15 * time.Second}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
		}

		// If job succeeded, update the status of the OdooDeployment
		if currentInitJob.Status.Succeeded > 0 {
			// The current init job has succeeded, so we can clear it
			logger.Info("Current InitJob succeeded, clearing")
			odooDeployment.Status.CurrentInitJob.Name = ""
			odooDeployment.Status.CurrentInitJob.Namespace = ""
			odooDeployment.Status.InitModulesInstalled = odooDeployment.Status.CurrentInitJob.Modules
			odooDeployment.Status.CurrentInitJob.Modules = []string{}
			updateStatus(odooDeployment, "OperatorSucceeded", "InitJobSucceeded", "InitJob succeeded, clearing", metav1.ConditionTrue)

			// Search for the pods with the label job-name = currentInitJob.Name
			pods := &corev1.PodList{}
			err = r.List(ctx, pods, client.MatchingLabels{"job-name": currentInitJob.Name})
			if err != nil {
				logger.Error(err, "Failed to list pods")
				updateStatus(odooDeployment, "OperatorDegraded", "FailedToListPods", fmt.Sprintf("Failed to list pods: %v", err), metav1.ConditionFalse)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
			}
			for _, pod := range pods.Items {
				// Delete the pod
				logger.Info(fmt.Sprintf("Deleting pod %s from job %s", pod.Name, currentInitJob.Name))
				err = r.Delete(ctx, &pod)
				if err != nil {
					logger.Error(err, "Failed to delete pod")
					updateStatus(odooDeployment, "OperatorDegraded", "FailedToDeletePod", fmt.Sprintf("Failed to delete pod: %v", err), metav1.ConditionFalse)
					return ctrl.Result{RequeueAfter: 30 * time.Second}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
				}
			}

			// Now we delete the Job
			logger.Info(fmt.Sprintf("Deleting job %s", currentInitJob.Name))
			err = r.Delete(ctx, currentInitJob)
			if err != nil {
				logger.Error(err, "Failed to delete current InitJob")
				updateStatus(odooDeployment, "OperatorDegraded", "FailedToDeleteInitJob", fmt.Sprintf("Failed to delete current InitJob: %v", err), metav1.ConditionFalse)
				return ctrl.Result{RequeueAfter: 30 * time.Second}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
			}

			return ctrl.Result{RequeueAfter: 30 * time.Second}, utilerrors.NewAggregate([]error{nil, r.Status().Update(ctx, odooDeployment)})
		} else if currentInitJob.Status.Active > 0 {
			// If job is still running, requeue
			logger.Info("Current InitJob still running, requening")
			return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
		}
		// If job failed, update the status of the OdooDeployment

	}
	// Check if InitModulesInstalled list matches the Spec.Modules list
	// If not, create a new InitJob to install the missing modules
	// If the list is empty, create a new InitJob to install all modules
	// If the list is the same, do nothing
	logger.Info(fmt.Sprintf("Currently installed modules: %d", len(odooDeployment.Status.InitModulesInstalled)))
	if len(odooDeployment.Status.InitModulesInstalled) == 0 || len(odooDeployment.Status.InitModulesInstalled) != len(odooDeployment.Spec.Modules) {
		// Create a new InitJob to install all modules
		logger.Info("Creating a new InitJob to install modules")

		initJob, modulesToInstall := odooDeployment.GetDbInitJobTemplate()
		ctrl.SetControllerReference(odooDeployment, &initJob, r.Scheme)

		err := r.Create(ctx, &initJob)
		if err != nil {
			logger.Error(err, fmt.Sprintf("error creating %s init job.", req.Name))
			updateStatus(odooDeployment, "OperatorSucceeded", "InitJobCreationFailed", fmt.Sprintf("error creating %s init job: %v", req.Name, err), metav1.ConditionFalse)
			return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
		}
		// Update the status of the OdooDeployment
		logger.Info(fmt.Sprintf("InitJob %s created", initJob.Name))
		odooDeployment.Status.CurrentInitJob = odoov1.DBInitjob{
			Name:      initJob.Name,
			Namespace: odooDeployment.Namespace,
			Modules:   modulesToInstall,
		}
		updateStatus(odooDeployment, "OperatorSucceeded", "InitJobCreated", fmt.Sprintf("InitJob %s created", initJob.Name), metav1.ConditionTrue)

		return ctrl.Result{RequeueAfter: 30 * time.Second}, utilerrors.NewAggregate([]error{nil, r.Status().Update(ctx, odooDeployment)})
	}

	// Check if the deployment already exists, if not create a new one
	deployment := appsv1.Deployment{}
	createDeployment := false
	err = r.Get(ctx, req.NamespacedName, &deployment)
	if err != nil && errors.IsNotFound(err) {
		// Create a new deployment for the OdooDeployment if it does not exist
		createDeployment = true
	} else if err != nil {
		// Error fetching deployment - requeue
		logger.Error(err, "Failed to get Deployment")
		updateStatus(odooDeployment, "OperatorDegraded", "FailedToGetDeployment", fmt.Sprintf("Failed to get Deployment: %v", err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	deployment = odooDeployment.GetDeploymentTemplate()
	ctrl.SetControllerReference(odooDeployment, &deployment, r.Scheme)

	if createDeployment {
		logger.Info(fmt.Sprintf("Creating a new Deployment for %s", req.Name))
		err = r.Create(ctx, &deployment)
	} else {
		err = r.Update(ctx, &deployment)
	}
	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s deployment.", req.Name))
		updateStatus(odooDeployment, "OperatorSucceeded", "DeploymentCreationFailed", fmt.Sprintf("error creating %s odoo deployment: %v", req.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	r.Status().Update(ctx, odooDeployment)

	// Check if the http service already exists, if not create a new one
	httpService := corev1.Service{}
	createHttpService := false
	httpServiceNamespacedName := types.NamespacedName{
		Name:      odooDeployment.GetHttpServiceName(),
		Namespace: odooDeployment.Namespace,
	}
	err = r.Get(ctx, httpServiceNamespacedName, &httpService)
	if err != nil && errors.IsNotFound(err) {
		createHttpService = true
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s service.", httpServiceNamespacedName.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonFailedGetHttpService, fmt.Sprintf("error creating %s service: %v", req.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	httpService = odooDeployment.GetHttpServiceTemplate()
	httpService.Name = httpServiceNamespacedName.Name
	httpService.Namespace = httpServiceNamespacedName.Namespace

	ctrl.SetControllerReference(odooDeployment, &httpService, r.Scheme)
	if createHttpService {
		logger.Info(fmt.Sprintf("Creating a new service for %s", req.Name))
		err = r.Create(ctx, &httpService)
	} else {
		err = r.Update(ctx, &httpService)
	}

	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating or updating %s service.", httpService.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonFailedCreateHttpService, fmt.Sprintf("error creating or updating %s service: %v", httpServiceNamespacedName.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	// Check if the http service already exists, if not create a new one
	pollService := corev1.Service{}
	createPollService := false
	pollServiceNamespacedName := types.NamespacedName{
		Name:      odooDeployment.GetPollServiceName(),
		Namespace: odooDeployment.Namespace,
	}
	err = r.Get(ctx, pollServiceNamespacedName, &pollService)
	if err != nil && errors.IsNotFound(err) {
		createPollService = true
	} else if err != nil {
		logger.Error(err, fmt.Sprintf("error creating %s service.", pollServiceNamespacedName.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonFailedGetPollService, fmt.Sprintf("error creating %s service: %v", pollServiceNamespacedName.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	pollService = odooDeployment.GetPollServiceTemplate()
	pollService.Name = pollServiceNamespacedName.Name
	pollService.Namespace = pollServiceNamespacedName.Namespace

	ctrl.SetControllerReference(odooDeployment, &pollService, r.Scheme)
	if createPollService {
		logger.Info(fmt.Sprintf("Creating a new service for %s", pollServiceNamespacedName.Name))
		err = r.Create(ctx, &pollService)
	} else {
		err = r.Update(ctx, &pollService)
	}

	if err != nil {
		logger.Error(err, fmt.Sprintf("error creating or updating %s service.", pollServiceNamespacedName.Name))
		updateStatus(odooDeployment, "OperatorDegraded", odoov1.ReasonFailedCreatePollService, fmt.Sprintf("error creating or updating %s service: %v", pollServiceNamespacedName.Name, err), metav1.ConditionFalse)
		return ctrl.Result{}, utilerrors.NewAggregate([]error{err, r.Status().Update(ctx, odooDeployment)})
	}

	logger.Info("Finished reconciling OdooDeployment")

	updateStatus(odooDeployment, "OperatorSucceeded", "ReconcileSucceeded", "Reconcile succeeded", metav1.ConditionTrue)
	return ctrl.Result{}, utilerrors.NewAggregate([]error{nil, r.Status().Update(ctx, odooDeployment)})
}

// SetupWithManager sets up the controller with the Manager.
func (r *OdooDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&odoov1.OdooDeployment{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}
