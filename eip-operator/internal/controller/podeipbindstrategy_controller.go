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
	"reflect"

	xslices "golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	v2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-ext-eip/api/v2"
	ipamv2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/go-logr/logr"
)

const (
	CCEEndpintStateReady = "ip-allocated"
	FeatureStatusEIPKey  = "eip"
)

// PodEIPBindStrategyReconciler reconciles a PodEIPBindStrategy object
type PodEIPBindStrategyReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cce.baidubce.com,resources=podeipbindstrategies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cce.baidubce.com,resources=podeipbindstrategies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cce.baidubce.com,resources=podeipbindstrategies/finalizers,verbs=update

//+kubebuilder:rbac:groups=cce.baidubce.com,resources=cceendpoints,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cce.baidubce.com,resources=cceendpoints/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cce.baidubce.com,resources=cceendpoints/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the PodEIPBindStrategy object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *PodEIPBindStrategyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	var pebs v2.PodEIPBindStrategy
	if err := r.Get(ctx, req.NamespacedName, &pebs); err != nil {
		if errors.IsNotFound(err) {
			log.Info("PEBS Reconcile, CR deleted")
			return ctrl.Result{}, client.IgnoreNotFound(err)
		}
		log.Error(err, "PEBS Reconcile, failed to Get PodEIPBindStrategy CR")
		return ctrl.Result{}, err
	}
	log.Info(fmt.Sprintf("PEBS Reconcile begin, CR %+v", pebs))

	if !xslices.Contains(pebs.Finalizers, FinalizerEIPKey) {
		if err := r.handlePEBSCreate(ctx, &log, &pebs); err != nil {
			return ctrl.Result{}, err
		}
	}

	if pebs.DeletionTimestamp != nil {
		return ctrl.Result{}, r.handlePEBSDelete(ctx, &log, &pebs)
	}

	if err := r.handleEIPs(ctx, &log, &pebs); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updatePEBSStatus(ctx, &log, &pebs); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.handleCCEEndpoints(ctx, &log, &pebs); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *PodEIPBindStrategyReconciler) handlePEBSCreate(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy) error {
	log.Info(fmt.Sprintf("PEBS Reconcile, handlePEBSCreate begin for [%s]", pebs.Namespace+"/"+pebs.Name))

	pebs.Finalizers = append(pebs.Finalizers, FinalizerEIPKey)
	if err := r.Update(ctx, pebs); err != nil {
		log.Error(err, "PEBS Reconcile, failed to add PEBS EIP Finalizer")
		return err
	}

	log.Info(fmt.Sprintf("PEBS Reconcile, PEBS EIP Finalizer added, new CR %+v", pebs))
	return nil
}

func (r *PodEIPBindStrategyReconciler) handlePEBSDelete(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy) error {
	log.Info(fmt.Sprintf("PEBS Reconcile, handlePEBSDelete begin for [%s]", pebs.Namespace+"/"+pebs.Name))

	for _, eip := range pebs.Spec.StaticEIPPool {
		var eipCR v2.EIP
		if err := r.Get(ctx, types.NamespacedName{Namespace: pebs.Namespace, Name: eip}, &eipCR); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to Get EIP CR for [%v]", eip))
			return err
		}
		if !IsEIPStatusStable(&eipCR) {
			log.Info(fmt.Sprintf("PEBS Reconcile, EIP [%s] status [%s, %s] is not stable, exit handlePEBSDelete",
				eipCR.Name, eipCR.Status.Status, eipCR.Status.Mode))
		}
		if eipCR.Status.Status == v2.EIPStatusTypeAvailable {
			if err := r.Client.Delete(ctx, &eipCR); err != nil {
				log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to delete EIP CR [%s]",
					eipCR.Namespace+"/"+eipCR.Name))
				return err
			}
			log.Info(fmt.Sprintf("PEBS Reconcile, deleted EIP CR [%s] due to PEBS delete, status [%s, %s]",
				eipCR.Namespace+"/"+eipCR.Name, eipCR.Status.Status, eipCR.Status.Mode))
		}
	}

	// 未使用状态的 EIP CR 都已经删除，其他 EIP 为正在使用中的，不能删除，否则 EIP 删除流程 UnBind EIP 会导致业务 Pod 流量故障
	// 此时可以删除 Finalizer
	var finalizers []string
	for _, f := range pebs.Finalizers {
		if f != FinalizerEIPKey {
			finalizers = append(finalizers, f)
		}
	}
	pebs.Finalizers = finalizers
	err := r.Update(ctx, pebs)
	if err != nil {
		log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to delete PEBS EIP finalizer for [%s], err: %+v",
			pebs.Namespace+"/"+pebs.Name, err))
		return err
	}
	log.Info(fmt.Sprintf("PEBS Reconcile, PEBS [%s] EIP finalizer deleted", pebs.Namespace+"/"+pebs.Name))
	return nil
}

func (r *PodEIPBindStrategyReconciler) handleEIPs(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy) error {
	log.Info("PEBS Reconcile, handleEIPs begin")

	for _, eip := range pebs.Spec.StaticEIPPool {
		var eipCR v2.EIP
		err := r.Get(ctx, types.NamespacedName{Namespace: pebs.Namespace, Name: eip}, &eipCR)
		if err == nil {
			// EIP CR exist, do nothing
			continue
		}
		if !errors.IsNotFound(err) {
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to Get EIP CR for [%v]", eip))
			return err
		}

		// EIP CR not exist, create it
		eipCRToCreate := &v2.EIP{
			ObjectMeta: metav1.ObjectMeta{
				Name:       eip,
				Namespace:  pebs.Namespace,
				Finalizers: []string{FinalizerEIPKey},
			},
			Spec: v2.EIPSpec{
				CreatedByCCE: true,
				Mode:         v2.EIPModeTypeDirect,
			},
		}
		if err := r.Client.Create(ctx, eipCRToCreate); err != nil {
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to Create EIP CR for [%v]", eip))
			return err
		}
		log.Info(fmt.Sprintf("PEBS Reconcile, EIP CR Created for [%s]", eip))
	}
	return nil
}

func (r *PodEIPBindStrategyReconciler) updatePEBSStatus(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy) error {
	log.Info("PEBS Reconcile, updatePEBSStatus begin")

	var eipList v2.EIPList
	if err := r.List(ctx, &eipList); err != nil {
		log.Error(err, "PEBS Reconcile, failed to List EIP")
		return err
	}

	newStatus := calculateStatus(pebs, &eipList)
	if reflect.DeepEqual(newStatus, pebs.Status) {
		log.Info("PEBS Reconcile, PEBS Status not change, do nothing")
		return nil
	}

	pebs.Status = newStatus
	if err := r.Status().Update(ctx, pebs); err != nil {
		return err
	}
	log.Info(fmt.Sprintf("PEBS Reconcile, PEBS Status updated, new CR %+v", pebs))
	return nil
}

func calculateStatus(pebs *v2.PodEIPBindStrategy, eipList *v2.EIPList) v2.PodEIPBindStrategyStatus {
	totalCount := len(pebs.Spec.StaticEIPPool)
	totalEIPs := map[string]map[string]string{}
	availableCount := 0
	availableEIPs := map[string]map[string]string{}
	bindedCound := 0
	bindedEIPs := map[string]map[string]string{}

	for _, eip := range eipList.Items {
		if !xslices.Contains(pebs.Spec.StaticEIPPool, eip.Name) {
			continue
		}

		totalEIPs[eip.Name] = map[string]string{}
		totalEIPs[eip.Name]["status"] = string(eip.Status.Status)
		totalEIPs[eip.Name]["mode"] = string(eip.Status.Mode)

		if eip.Status.Status == v2.EIPStatusTypeAvailable {
			availableCount += 1
			availableEIPs[eip.Name] = map[string]string{}
			availableEIPs[eip.Name]["status"] = string(eip.Status.Status)
			availableEIPs[eip.Name]["mode"] = string(eip.Status.Mode)
		} else if eip.Status.Status == v2.EIPStatusTypeBinded {
			bindedCound += 1
			bindedEIPs[eip.Name] = map[string]string{}
			bindedEIPs[eip.Name]["status"] = string(eip.Status.Status)
			bindedEIPs[eip.Name]["mode"] = string(eip.Status.Mode)
			bindedEIPs[eip.Name]["instanceId"] = eip.Status.InstanceID
		}
	}

	return v2.PodEIPBindStrategyStatus{
		TotalCount:     totalCount,
		TotalEIPs:      totalEIPs,
		AvailableCount: availableCount,
		AvailableEIPs:  availableEIPs,
		BindedCount:    bindedCound,
		BindedEIPs:     bindedEIPs,
	}
}

func (r *PodEIPBindStrategyReconciler) handleCCEEndpoints(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy) error {
	log.Info("PEBS Reconcile, handleCCEEndpoints begin")

	ceps := r.getCEPsForPEBS(ctx, log, pebs)
	for _, cep := range ceps {
		if cep.DeletionTimestamp != nil {
			if err := r.handleCCEEndpointDelete(ctx, log, &cep); err != nil {
				return err
			}
			continue
		}

		if err := r.handleCCEEndpoint(ctx, log, pebs, &cep); err != nil {
			return err
		}
	}
	return nil
}

func (r *PodEIPBindStrategyReconciler) handleCCEEndpointDelete(
	ctx context.Context,
	log *logr.Logger,
	cep *ipamv2.CCEEndpoint) error {
	log.Info(fmt.Sprintf("PEBS Reconcile, handleCCEEndpointDelete begin for [%s]", cep.Namespace+"/"+cep.Name))

	if !hasEIPFinalizer(cep) {
		log.Info(fmt.Sprintf("PEBS Reconcile, CCEEndpoint [%s] has no EIP finalizer, skip",
			cep.Namespace+"/"+cep.Name))
		return nil
	}

	eipStr := getEIPFromCCEEndpoint(cep)
	var eipCR v2.EIP
	if eipStr != "" {
		if err := r.Client.Get(ctx, types.NamespacedName{Namespace: cep.Namespace, Name: eipStr}, &eipCR); err != nil {
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to get EIP CR [%s]", eipStr))
			return err
		}
		if err := r.Client.Delete(ctx, &eipCR); err != nil {
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to delete EIP CR [%s]",
				eipCR.Namespace+"/"+eipCR.Name))
			return err
		}
		log.Info(fmt.Sprintf("PEBS Reconcile, deleted EIP CR [%s] due to CCEEndopint delete",
			eipCR.Namespace+"/"+eipCR.Name))
	}

	var finalizers []string
	for _, f := range cep.Finalizers {
		if f != v2.FeatureKeyOfPublicIP {
			finalizers = append(finalizers, f)
		}
	}
	cep.Finalizers = finalizers
	err := r.Client.Update(ctx, cep)
	if err != nil {
		log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to Update CCEEndpoint finalizer for [%s], err: %+v",
			cep.Namespace+"/"+cep.Name, err))
		return err
	}

	log.Info(fmt.Sprintf("PEBS Reconcile, CCEEndpoint [%s] EIP finalizer deleted", cep.Namespace+"/"+cep.Name))
	return nil
}

func (r *PodEIPBindStrategyReconciler) handleCCEEndpoint(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy,
	cep *ipamv2.CCEEndpoint) error {
	log.Info(fmt.Sprintf("PEBS Reconcile, handleCCEEndpoint begin for [%s]", cep.Namespace+"/"+cep.Name))

	if cep.Status.State != CCEEndpintStateReady {
		log.Info(fmt.Sprintf("PEBS Reconcile, CCEEndpoint [%s] state is [%s], not ready, skip handleCCEEndpoint",
			cep.Namespace+"/"+cep.Name, cep.Status.State))
		return nil
	}

	eipStr := getEIPFromCCEEndpoint(cep)
	if eipStr == "" {
		// need allocate a new EIP
		if pebs.Status.AvailableCount == 0 {
			return fmt.Errorf("no available EIP")
		}

		// allocate EIP
		for k := range pebs.Status.AvailableEIPs {
			eipStr = k
			break
		}

		// update CCEEndpoint Status
		cep.Status.ExtFeatureStatus = map[string]*ipamv2.ExtFeatureStatus{
			v2.FeatureKeyOfPublicIP: {
				Ready:       false,
				ContainerID: cep.Status.ExternalIdentifiers.ContainerID,
				Data: map[string]string{
					"eip":  eipStr,
					"mode": "direct",
				},
			},
		}
		if err := r.Client.Update(ctx, cep); err != nil {
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to update CCEEndpoint Status for [%s]",
				cep.Namespace+"/"+cep.Name))
			return err
		}
		log.Info(fmt.Sprintf("PEBS Reconcile, EIP [%s] Allocated for CCEEndpoint [%s]",
			eipStr, cep.Namespace+"/"+cep.Name))

		// get the updated new CCEEndpoint
		if err := r.Get(ctx, types.NamespacedName{Namespace: cep.Namespace, Name: cep.Name}, cep); err != nil {
			if errors.IsNotFound(err) {
				log.Info(fmt.Sprintf("PEBS Reconcile, new CCEEndpoint CR [%s] deleted", cep.Namespace+"/"+cep.Name))
				return client.IgnoreNotFound(err)
			}
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to Get new CCEEndpoint CR [%s]",
				cep.Namespace+"/"+cep.Name))
			return err
		}
	}

	// update EIP Status
	var eipCR v2.EIP
	if err := r.Client.Get(ctx, types.NamespacedName{Namespace: pebs.Namespace, Name: eipStr}, &eipCR); err != nil {
		log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to get EIP CR for [%s]", eipStr))
		return err
	}

	if eipCR.Status.Status == v2.EIPStatusTypeAvailable &&
		eipCR.Status.InstanceID == "" &&
		eipCR.Status.PrivateIP == "" {
		eipCR.Status.Endpoint = cep.Namespace + "/" + cep.Name
		eipCR.Status.InstanceID = cep.Status.Networking.Addressing[0].Interface
		eipCR.Status.PrivateIP = cep.Status.Networking.Addressing[0].IP
		return r.updateEIPStatus(ctx, log, &eipCR)
	}

	if IsEIPBindReady(&eipCR) && !cep.Status.ExtFeatureStatus[v2.FeatureKeyOfPublicIP].Ready {
		// EIP Binded, mark EIP Ready in CCEEndpoint Status
		cep.Status.ExtFeatureStatus[v2.FeatureKeyOfPublicIP].Ready = true
		if err := r.Client.Update(ctx, cep); err != nil {
			log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to mark Binded EIP [%s] Ready for CCEEndpoint [%s]",
				eipStr, cep.Namespace+"/"+cep.Name))
			return err
		}
		log.Info(fmt.Sprintf("PEBS Reconcile, EIP [%s] Binded, mark it Ready for CCEEndpoint [%s]",
			eipStr, cep.Namespace+"/"+cep.Name))
	}

	return nil
}

func (r *PodEIPBindStrategyReconciler) updateEIPStatus(
	ctx context.Context,
	log *logr.Logger,
	eip *v2.EIP) error {
	if err := r.Client.Status().Update(ctx, eip); err != nil {
		log.Error(err, fmt.Sprintf("PEBS Reconcile, failed to update EIP CR [%s]", eip.Name))
		return err
	}
	log.Info(fmt.Sprintf("PEBS Reconcile, EIP [%s] status updated to %+v", eip.Name, eip.Status))
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodEIPBindStrategyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v2.PodEIPBindStrategy{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&source.Kind{Type: &ipamv2.CCEEndpoint{}},
			handler.EnqueueRequestsFromMapFunc(r.mapCEPToPEBS),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&source.Kind{Type: &v2.EIP{}},
			handler.EnqueueRequestsFromMapFunc(r.mapEIPToPEBS),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// TODO: retry if something failed
func (r *PodEIPBindStrategyReconciler) mapCEPToPEBS(cepObj client.Object) []reconcile.Request {
	cep := cepObj.(*ipamv2.CCEEndpoint)

	if !isEIPEnabled(cep) {
		return nil
	}

	pebss := r.getPEBSsForCep(cep)

	var requests []reconcile.Request
	for _, pebs := range pebss {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      pebs.Name,
				Namespace: pebs.Namespace,
			}})
	}

	return requests
}

// TODO: retry if something failed
func (r *PodEIPBindStrategyReconciler) mapEIPToPEBS(cepObj client.Object) []reconcile.Request {
	eip := cepObj.(*v2.EIP)

	pebss := r.getPEBSsForEIP(eip)

	var requests []reconcile.Request
	for _, pebs := range pebss {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      pebs.Name,
				Namespace: pebs.Namespace,
			}})
	}

	return requests
}

func (r *PodEIPBindStrategyReconciler) getCEPsForPEBS(
	ctx context.Context,
	log *logr.Logger,
	pebs *v2.PodEIPBindStrategy) []ipamv2.CCEEndpoint {

	if pebs.Spec.Selector == nil {
		return nil
	}

	var cepList ipamv2.CCEEndpointList
	sel, err := labels.Parse(metav1.FormatLabelSelector(pebs.Spec.Selector))
	if err != nil {
		log.Error(err, "PEBS Reconcile, failed to parse PEBS Selector", "PEBS Selector", pebs.Spec.Selector)
		return nil
	}
	if err := r.List(ctx, &cepList, client.InNamespace(pebs.Namespace),
		&client.ListOptions{LabelSelector: sel}); err != nil {
		log.Error(err, "PEBS Reconcile, failed to List CCEEndpoint")
		return nil
	}

	var res []ipamv2.CCEEndpoint
	for _, cep := range cepList.Items {
		if isEIPEnabled(&cep) {
			res = append(res, cep)
		}
	}

	namespacedNameOfCeps := []string{}
	for _, cep := range res {
		namespacedNameOfCeps = append(namespacedNameOfCeps, cep.Namespace+"/"+cep.Name)
	}
	log.Info(fmt.Sprintf("PEBS Reconcile, got %v matched CCEEndpoints", len(namespacedNameOfCeps)),
		"CCEEndpoints", namespacedNameOfCeps)

	return res
}

func (r *PodEIPBindStrategyReconciler) getPEBSsForCep(cep *ipamv2.CCEEndpoint) []v2.PodEIPBindStrategy {
	pebsList := v2.PodEIPBindStrategyList{}
	if err := r.Client.List(context.TODO(), &pebsList, client.InNamespace(cep.Namespace)); err != nil {
		return nil
	}

	res := []v2.PodEIPBindStrategy{}
	for i := 0; i < len(pebsList.Items); i++ {
		if v2.PebsMatchCep(&pebsList.Items[i], cep) {
			res = append(res, pebsList.Items[i])
		}
	}

	return res
}

func (r *PodEIPBindStrategyReconciler) getPEBSsForEIP(eip *v2.EIP) []v2.PodEIPBindStrategy {
	pebsList := v2.PodEIPBindStrategyList{}
	if err := r.Client.List(context.TODO(), &pebsList, client.InNamespace(eip.Namespace)); err != nil {
		return nil
	}

	res := []v2.PodEIPBindStrategy{}
	for i := 0; i < len(pebsList.Items); i++ {
		if xslices.Contains(pebsList.Items[i].Spec.StaticEIPPool, eip.Name) {
			res = append(res, pebsList.Items[i])
		}
	}

	return res
}

func getEIPFromCCEEndpoint(cep *ipamv2.CCEEndpoint) string {
	if cep.Status.ExtFeatureStatus == nil {
		return ""
	}
	status, ok := cep.Status.ExtFeatureStatus[v2.FeatureKeyOfPublicIP]
	if !ok {
		return ""
	}
	if status.Data == nil {
		return ""
	}
	eip, ok := status.Data[FeatureStatusEIPKey]
	if !ok {
		return ""
	}

	return eip
}

func isEIPEnabled(cep *ipamv2.CCEEndpoint) bool {
	return xslices.Contains(cep.Spec.ExtFeatureGates, v2.FeatureKeyOfPublicIP)
}

func hasEIPFinalizer(cep *ipamv2.CCEEndpoint) bool {
	return xslices.Contains(cep.Finalizers, v2.FeatureKeyOfPublicIP)
}
