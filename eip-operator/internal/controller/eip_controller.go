/*
Copyright (c) 2023 Baidu, Inc. All Rights Reserved.

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

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-ext-eip/api/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/cloud"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	vpceip "github.com/baidubce/bce-sdk-go/services/eip"
	"github.com/go-logr/logr"
)

const (
	// This Finalizer key is used to unbind EIP before EIP resource deletion
	FinalizerEIPKey = "EIP"
)

// EIPReconciler reconciles a EIP object
type EIPReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Region       string
	CCEClusterID string
	bceclient    cloud.Interface
}

//+kubebuilder:rbac:groups=cce.baidubce.com,resources=eips,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cce.baidubce.com,resources=eips/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cce.baidubce.com,resources=eips/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the EIP object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *EIPReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var eip v2.EIP
	if err := r.Get(ctx, req.NamespacedName, &eip); err != nil {
		if errors.IsNotFound(err) {
			log.Info("EIP Reconcile, CR deleted")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		log.Error(err, "EIP Reconcile, failed to Get EIP CR")
		return ctrl.Result{}, err
	}
	log.Info(fmt.Sprintf("EIP Reconcile begin, CR %+v", eip))

	switch status := eip.Status.Status; status {
	case v2.EIPStatusTypeEmpty:
		return r.onStatusEmpty(ctx, &log, &eip)
	case v2.EIPStatusTypeAvailable:
		return r.onStatusAvailable(ctx, &log, &eip)
	case v2.EIPStatusTypeBinding:
		return r.onStatusBinding(ctx, &log, &eip)
	case v2.EIPStatusTypeBinded:
		return r.onStatusBinded(ctx, &log, &eip)
	case v2.EIPStatusTypeUnBinding:
		return r.onStatusUnBinding(ctx, &log, &eip)
	case v2.EIPStatusTypeUnBinded:
		return r.onStatusUnBinded(ctx, &log, &eip)
	default:
		log.Info(fmt.Sprintf("EIP Reconcile, nothing todo with EIP Status [status:%s, mode:%s], exit",
			status, eip.Status.Mode))
		return ctrl.Result{}, nil
	}
}

func (r *EIPReconciler) onStatusEmpty(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info("EIP Reconcile, onStatusEmpty")

	vpcEIP, err := r.getEIPInfoFromVPC(ctx, log, eip)
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, err
	}

	eip.Status.Status = v2.EIPStatusTypeAvailable
	eip.Status.Mode = v2.EIPModeTypeNAT
	return ctrl.Result{}, r.updateEIPFromVPC(ctx, log, eip, &vpcEIP)
}

func (r *EIPReconciler) onStatusAvailable(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info("EIP Reconcile, onStatusAvailable")

	if eip.DeletionTimestamp != nil {
		return r.handleEIPDelete(ctx, log, eip)
	}

	if eip.Status.InstanceID != "" && eip.Status.PrivateIP != "" {
		eip.Status.Status = v2.EIPStatusTypeBinding
		return ctrl.Result{}, r.updateEIPStatus(ctx, log, eip)
	}

	return ctrl.Result{}, nil
}

func (r *EIPReconciler) onStatusBinding(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info("EIP Reconcile, onStatusBinding")

	if eip.Status.InstanceID == "" {
		log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to Bind EIP [%s], lack InstanceID", eip.Name)), "")
		return ctrl.Result{}, nil
	}
	if eip.Status.PrivateIP == "" {
		log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to Bind EIP [%s], lack PrivateIP", eip.Name)), "")
		return ctrl.Result{}, nil
	}

	// Bind EIP
	// TODO: check wheather the EIP has been Binded
	if err := r.bceclient.BindENIPublicIP(ctx, eip.Status.PrivateIP, eip.Name, eip.Status.InstanceID); err != nil {
		log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to Bind EIP [%s] to [%s] in VPC",
			eip.Name, eip.Status.InstanceID)), "")
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, err
	}
	log.Info(fmt.Sprintf("EIP Reconcile, EIP [%s] Binded to [%s] in VPC", eip.Name, eip.Status.InstanceID))

	eip.Status.Status = v2.EIPStatusTypeBinded
	return ctrl.Result{}, r.updateEIPStatus(ctx, log, eip)
}

func (r *EIPReconciler) onStatusBinded(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info("EIP Reconcile, onStatusBinded")

	// Direct EIP
	// TODO: check wheather the EIP has been Directed
	if eip.Spec.Mode == v2.EIPModeTypeDirect && eip.Status.Mode != v2.EIPModeTypeDirect {
		if err := r.bceclient.DirectEIP(ctx, eip.Name); err != nil {
			log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to Direct EIP [%s] in VPC", eip.Name)), "")
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, err
		}
		log.Info(fmt.Sprintf("EIP Reconcile, EIP [%s] Directed in VPC", eip.Name))

		eip.Status.Mode = v2.EIPModeTypeDirect
		return ctrl.Result{}, r.updateEIPStatus(ctx, log, eip)
	}

	// update EIP from VPC
	vpcEIP, err := r.getEIPInfoFromVPC(ctx, log, eip)
	if err != nil {
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, err
	}
	if err := r.updateEIPFromVPC(ctx, log, eip, &vpcEIP); err != nil {
		return ctrl.Result{}, err
	}

	if eip.DeletionTimestamp != nil {
		return r.handleEIPDelete(ctx, log, eip)
	}
	return ctrl.Result{}, nil
}

func (r *EIPReconciler) onStatusUnBinding(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info("EIP Reconcile, onStatusUnBinding")

	// Undirect EIP
	// TODO: check wheather the EIP has been Directed
	// TODO: wheather it's needed to UnDirect EIP before Unbind?
	if eip.Status.Mode == v2.EIPModeTypeDirect {
		if err := r.bceclient.UnDirectEIP(ctx, eip.Name); err != nil {
			log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to UnDirect EIP [%s] in VPC", eip.Name)), "")
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, err
		}
		log.Info(fmt.Sprintf("EIP Reconcile, EIP [%s] UnDirected in VPC", eip.Name))

		eip.Status.Mode = v2.EIPModeTypeNAT
		return ctrl.Result{}, r.updateEIPStatus(ctx, log, eip)
	}

	// Unbind EIP
	// TODO: check wheather the EIP has been Binded
	if eip.Status.InstanceID == "" {
		log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to Unbind EIP [%s], lack InstanceID", eip.Name)), "")
		return ctrl.Result{}, nil
	}
	if err := r.bceclient.UnBindENIPublicIP(ctx, eip.Name, eip.Status.InstanceID); err != nil {
		log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to bind EIP [%s] from [%s] in VPC",
			eip.Name, eip.Status.InstanceID)), "")
		return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, err
	}
	log.Info(fmt.Sprintf("EIP Reconcile, EIP [%s] Unbinded from [%s] in VPC", eip.Name, eip.Status.InstanceID))

	eip.Status.Status = v2.EIPStatusTypeUnBinded
	return ctrl.Result{}, r.updateEIPStatus(ctx, log, eip)
}

func (r *EIPReconciler) onStatusUnBinded(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info("EIP Reconcile, onStatusUnBinded")

	if eip.DeletionTimestamp != nil {
		return r.handleEIPDelete(ctx, log, eip)
	}

	return ctrl.Result{}, nil
}

func (r *EIPReconciler) handleEIPDelete(ctx context.Context, log *logr.Logger, eip *v2.EIP) (ctrl.Result, error) {
	log.Info(fmt.Sprintf("EIP Reconcile, handleEIPDelete for [%s] of status [%s]",
		eip.Namespace+"/"+eip.Name, eip.Status.Status))

	if eip.Status.Status == v2.EIPStatusTypeBinded {
		eip.Status.Status = v2.EIPStatusTypeUnBinding
		return ctrl.Result{}, r.updateEIPStatus(ctx, log, eip)
	}

	if eip.Status.Status == v2.EIPStatusTypeUnBinded ||
		eip.Status.Status == v2.EIPStatusTypeAvailable {
		var finalizers []string
		for _, f := range eip.Finalizers {
			if f != FinalizerEIPKey {
				finalizers = append(finalizers, f)
			}
		}
		eip.Finalizers = finalizers
		err := r.Update(ctx, eip)
		if err != nil {
			log.Error(fmt.Errorf(fmt.Sprintf(
				"EIP Reconcile, failed to Delete EIP finalizer for [%s] of status [%s], err: %+v",
				eip.Namespace+"/"+eip.Name, eip.Status.Status, err)), "")
			return ctrl.Result{}, err
		}

		log.Info(fmt.Sprintf("EIP Reconcile, EIP finalizer deleted for [%s]  of status [%s]",
			eip.Namespace+"/"+eip.Name, eip.Status.Status))
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *EIPReconciler) getEIPInfoFromVPC(ctx context.Context, log *logr.Logger, eip *v2.EIP) (vpceip.EipModel, error) {
	listEIPArgs := vpceip.ListEipArgs{
		Eip: eip.Name,
	}
	if vpcEIPs, err := r.bceclient.ListEIPs(ctx, listEIPArgs); err != nil {
		log.Error(fmt.Errorf(fmt.Sprintf("EIP Reconcile, failed to ListEIPs for [%s] in VPC, err: %v",
			eip.Name, err)), "")
		return vpceip.EipModel{}, err
	} else {
		log.Info(fmt.Sprintf("EIP Reconcile, got EIP info from VPC %+v", vpcEIPs[0]))
		return vpcEIPs[0], nil
	}
}

func (r *EIPReconciler) updateEIPFromVPC(
	ctx context.Context,
	log *logr.Logger,
	eip *v2.EIP,
	vpcEIP *vpceip.EipModel) error {

	eip.Status.StatusInVPC = vpcEIP.Status
	eip.Status.InstanceType = vpcEIP.InstanceType
	eip.Status.ExpireTime = vpcEIP.ExpireTime

	if err := r.updateEIPStatus(ctx, log, eip); err != nil {
		return err
	}

	eip.Spec.Name = vpcEIP.Name
	eip.Spec.EIP = vpcEIP.Eip
	eip.Spec.EIPID = vpcEIP.EipId
	eip.Spec.ShareGroupID = vpcEIP.ShareGroupId
	eip.Spec.ClusterID = vpcEIP.ClusterId
	eip.Spec.BandWidthInMbps = vpcEIP.BandWidthInMbps
	eip.Spec.PaymentTiming = vpcEIP.PaymentTiming
	eip.Spec.BillingMethod = vpcEIP.BillingMethod
	eip.Spec.CreateTime = vpcEIP.CreateTime
	eip.Spec.Tags = vpcEIP.Tags

	if err := r.Update(ctx, eip); err != nil {
		log.Error(err, fmt.Sprintf("EIP Reconcile, failed to update EIP spec for %s", eip.Name))
		return err
	}
	log.Info(fmt.Sprintf("EIP Reconcile, EIP [%s] spec updated to %+v", eip.Name, eip.Spec))
	return nil
}

func (r *EIPReconciler) updateEIPStatus(
	ctx context.Context,
	log *logr.Logger,
	eip *v2.EIP) error {
	if err := r.Status().Update(ctx, eip); err != nil {
		log.Error(err, fmt.Sprintf(
			"EIP Reconcile, failed to update EIP status for [%s]", eip.Name))
		return err
	}
	log.Info(fmt.Sprintf("EIP Reconcile, EIP [%s] status updated to [%+v]", eip.Name, eip.Status))
	return nil
}

func IsEIPStatusStable(eip *v2.EIP) bool {
	if eip.Status.Status == v2.EIPStatusTypeAvailable && eip.Status.InstanceID == "" && eip.Status.PrivateIP == "" ||
		eip.Status.Status == v2.EIPStatusTypeBinded && eip.Status.Mode == eip.Spec.Mode {
		return true
	}
	return false
}

func IsEIPBindReady(eip *v2.EIP) bool {
	return eip.Status.Status == v2.EIPStatusTypeBinded && eip.Spec.Mode == eip.Status.Mode
}

// SetupWithManager sets up the controller with the Manager.
func (r *EIPReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.initBCEClient(); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v2.EIP{}).
		Complete(r)
}

func (r *EIPReconciler) initBCEClient() error {
	c, err := cloud.New(
		r.Region,       /*Region*/
		r.CCEClusterID, /*CCE Cluseter ID*/
		"",             /*BCE Access Key*/
		"",             /*BCE Secure Key*/
		k8s.Client(),
		false,
	)
	if err != nil {
		return err
	}

	c, err = cloud.NewFlowControlClient(
		c,
		5,  /*DefaultAPIQPSLimit*/
		10, /*DefaultAPIBurst*/
		15, /*DefaultAPITimeoutLimit*/
	)
	if err != nil {
		return err
	}

	r.bceclient = c
	return nil
}
