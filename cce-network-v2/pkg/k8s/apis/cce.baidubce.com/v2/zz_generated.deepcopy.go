//go:build !ignore_autogenerated
// +build !ignore_autogenerated

/*
 * Copyright (c) 2023 Baidu, Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 *
 */

// Code generated by deepcopy-gen. DO NOT EDIT.

package v2

import (
	models "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	api "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	privatecloudbaseapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/privatecloudbase/api"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *AddressPair) DeepCopyInto(out *AddressPair) {
	*out = *in
	if in.CIDRs != nil {
		in, out := &in.CIDRs, &out.CIDRs
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AddressPair.
func (in *AddressPair) DeepCopy() *AddressPair {
	if in == nil {
		return nil
	}
	out := new(AddressPair)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in AddressPairList) DeepCopyInto(out *AddressPairList) {
	{
		in := &in
		*out = make(AddressPairList, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(AddressPair)
				(*in).DeepCopyInto(*out)
			}
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new AddressPairList.
func (in AddressPairList) DeepCopy() AddressPairList {
	if in == nil {
		return nil
	}
	out := new(AddressPairList)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *BindwidthOption) DeepCopyInto(out *BindwidthOption) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new BindwidthOption.
func (in *BindwidthOption) DeepCopy() *BindwidthOption {
	if in == nil {
		return nil
	}
	out := new(BindwidthOption)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CCEEndpoint) DeepCopyInto(out *CCEEndpoint) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CCEEndpoint.
func (in *CCEEndpoint) DeepCopy() *CCEEndpoint {
	if in == nil {
		return nil
	}
	out := new(CCEEndpoint)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CCEEndpoint) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CCEEndpointList) DeepCopyInto(out *CCEEndpointList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]CCEEndpoint, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CCEEndpointList.
func (in *CCEEndpointList) DeepCopy() *CCEEndpointList {
	if in == nil {
		return nil
	}
	out := new(CCEEndpointList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *CCEEndpointList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CodeError) DeepCopyInto(out *CodeError) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CodeError.
func (in *CodeError) DeepCopy() *CodeError {
	if in == nil {
		return nil
	}
	out := new(CodeError)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in CodeErrorList) DeepCopyInto(out *CodeErrorList) {
	{
		in := &in
		*out = make(CodeErrorList, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(CodeError)
				**out = **in
			}
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CodeErrorList.
func (in CodeErrorList) DeepCopy() CodeErrorList {
	if in == nil {
		return nil
	}
	out := new(CodeErrorList)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in ControllerList) DeepCopyInto(out *ControllerList) {
	{
		in := &in
		*out = make(ControllerList, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ControllerList.
func (in ControllerList) DeepCopy() ControllerList {
	if in == nil {
		return nil
	}
	out := new(ControllerList)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ControllerStatus) DeepCopyInto(out *ControllerStatus) {
	*out = *in
	if in.Configuration != nil {
		in, out := &in.Configuration, &out.Configuration
		*out = new(models.ControllerStatusConfiguration)
		**out = **in
	}
	out.Status = in.Status
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ControllerStatus.
func (in *ControllerStatus) DeepCopy() *ControllerStatus {
	if in == nil {
		return nil
	}
	out := new(ControllerStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ControllerStatusStatus) DeepCopyInto(out *ControllerStatusStatus) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ControllerStatusStatus.
func (in *ControllerStatusStatus) DeepCopy() *ControllerStatusStatus {
	if in == nil {
		return nil
	}
	out := new(ControllerStatusStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CustomAllocation) DeepCopyInto(out *CustomAllocation) {
	*out = *in
	if in.Range != nil {
		in, out := &in.Range, &out.Range
		*out = make([]CustomIPRange, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CustomAllocation.
func (in *CustomAllocation) DeepCopy() *CustomAllocation {
	if in == nil {
		return nil
	}
	out := new(CustomAllocation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in CustomAllocationList) DeepCopyInto(out *CustomAllocationList) {
	{
		in := &in
		*out = make(CustomAllocationList, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
		return
	}
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CustomAllocationList.
func (in CustomAllocationList) DeepCopy() CustomAllocationList {
	if in == nil {
		return nil
	}
	out := new(CustomAllocationList)
	in.DeepCopyInto(out)
	return *out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *CustomIPRange) DeepCopyInto(out *CustomIPRange) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new CustomIPRange.
func (in *CustomIPRange) DeepCopy() *CustomIPRange {
	if in == nil {
		return nil
	}
	out := new(CustomIPRange)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *DirectIPAllocation) DeepCopyInto(out *DirectIPAllocation) {
	*out = *in
	if in.TTLSecondsAfterDeleted != nil {
		in, out := &in.TTLSecondsAfterDeleted, &out.TTLSecondsAfterDeleted
		*out = new(int64)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new DirectIPAllocation.
func (in *DirectIPAllocation) DeepCopy() *DirectIPAllocation {
	if in == nil {
		return nil
	}
	out := new(DirectIPAllocation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ENI) DeepCopyInto(out *ENI) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ENI.
func (in *ENI) DeepCopy() *ENI {
	if in == nil {
		return nil
	}
	out := new(ENI)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ENI) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ENIList) DeepCopyInto(out *ENIList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]ENI, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ENIList.
func (in *ENIList) DeepCopy() *ENIList {
	if in == nil {
		return nil
	}
	out := new(ENIList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ENIList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ENISpec) DeepCopyInto(out *ENISpec) {
	*out = *in
	in.ENI.DeepCopyInto(&out.ENI)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ENISpec.
func (in *ENISpec) DeepCopy() *ENISpec {
	if in == nil {
		return nil
	}
	out := new(ENISpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ENIStatus) DeepCopyInto(out *ENIStatus) {
	*out = *in
	if in.EndpointReference != nil {
		in, out := &in.EndpointReference, &out.EndpointReference
		*out = new(ObjectReference)
		**out = **in
	}
	if in.CCEStatusChangeLog != nil {
		in, out := &in.CCEStatusChangeLog, &out.CCEStatusChangeLog
		*out = make([]ENIStatusChange, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.VPCStatusChangeLog != nil {
		in, out := &in.VPCStatusChangeLog, &out.VPCStatusChangeLog
		*out = make([]ENIStatusChange, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ENIStatus.
func (in *ENIStatus) DeepCopy() *ENIStatus {
	if in == nil {
		return nil
	}
	out := new(ENIStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ENIStatusChange) DeepCopyInto(out *ENIStatusChange) {
	*out = *in
	in.StatusChange.DeepCopyInto(&out.StatusChange)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ENIStatusChange.
func (in *ENIStatusChange) DeepCopy() *ENIStatusChange {
	if in == nil {
		return nil
	}
	out := new(ENIStatusChange)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EgressPriorityOpt) DeepCopyInto(out *EgressPriorityOpt) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EgressPriorityOpt.
func (in *EgressPriorityOpt) DeepCopy() *EgressPriorityOpt {
	if in == nil {
		return nil
	}
	out := new(EgressPriorityOpt)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EncryptionSpec) DeepCopyInto(out *EncryptionSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EncryptionSpec.
func (in *EncryptionSpec) DeepCopy() *EncryptionSpec {
	if in == nil {
		return nil
	}
	out := new(EncryptionSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EndpointNetworkSpec) DeepCopyInto(out *EndpointNetworkSpec) {
	*out = *in
	if in.IPAllocation != nil {
		in, out := &in.IPAllocation, &out.IPAllocation
		*out = new(IPAllocation)
		(*in).DeepCopyInto(*out)
	}
	if in.Bindwidth != nil {
		in, out := &in.Bindwidth, &out.Bindwidth
		*out = new(BindwidthOption)
		**out = **in
	}
	if in.EgressPriority != nil {
		in, out := &in.EgressPriority, &out.EgressPriority
		*out = new(EgressPriorityOpt)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EndpointNetworkSpec.
func (in *EndpointNetworkSpec) DeepCopy() *EndpointNetworkSpec {
	if in == nil {
		return nil
	}
	out := new(EndpointNetworkSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EndpointNetworking) DeepCopyInto(out *EndpointNetworking) {
	*out = *in
	if in.Addressing != nil {
		in, out := &in.Addressing, &out.Addressing
		*out = make(AddressPairList, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(AddressPair)
				(*in).DeepCopyInto(*out)
			}
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EndpointNetworking.
func (in *EndpointNetworking) DeepCopy() *EndpointNetworking {
	if in == nil {
		return nil
	}
	out := new(EndpointNetworking)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EndpointSpec) DeepCopyInto(out *EndpointSpec) {
	*out = *in
	if in.ExternalIdentifiers != nil {
		in, out := &in.ExternalIdentifiers, &out.ExternalIdentifiers
		*out = new(models.EndpointIdentifiers)
		**out = **in
	}
	in.Network.DeepCopyInto(&out.Network)
	if in.ExtFeatureGates != nil {
		in, out := &in.ExtFeatureGates, &out.ExtFeatureGates
		*out = make([]string, len(*in))
		copy(*out, *in)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EndpointSpec.
func (in *EndpointSpec) DeepCopy() *EndpointSpec {
	if in == nil {
		return nil
	}
	out := new(EndpointSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *EndpointStatus) DeepCopyInto(out *EndpointStatus) {
	*out = *in
	if in.Controllers != nil {
		in, out := &in.Controllers, &out.Controllers
		*out = make(ControllerList, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ExternalIdentifiers != nil {
		in, out := &in.ExternalIdentifiers, &out.ExternalIdentifiers
		*out = new(models.EndpointIdentifiers)
		**out = **in
	}
	if in.Log != nil {
		in, out := &in.Log, &out.Log
		*out = make([]*models.EndpointStatusChange, len(*in))
		for i := range *in {
			if (*in)[i] != nil {
				in, out := &(*in)[i], &(*out)[i]
				*out = new(models.EndpointStatusChange)
				**out = **in
			}
		}
	}
	if in.Networking != nil {
		in, out := &in.Networking, &out.Networking
		*out = new(EndpointNetworking)
		(*in).DeepCopyInto(*out)
	}
	if in.NodeSelectorRequirement != nil {
		in, out := &in.NodeSelectorRequirement, &out.NodeSelectorRequirement
		*out = make([]v1.NodeSelectorRequirement, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.ExtFeatureStatus != nil {
		in, out := &in.ExtFeatureStatus, &out.ExtFeatureStatus
		*out = make(map[string]*ExtFeatureStatus, len(*in))
		for key, val := range *in {
			var outVal *ExtFeatureStatus
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = new(ExtFeatureStatus)
				(*in).DeepCopyInto(*out)
			}
			(*out)[key] = outVal
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new EndpointStatus.
func (in *EndpointStatus) DeepCopy() *EndpointStatus {
	if in == nil {
		return nil
	}
	out := new(EndpointStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ExtFeatureStatus) DeepCopyInto(out *ExtFeatureStatus) {
	*out = *in
	if in.UpdateTime != nil {
		in, out := &in.UpdateTime, &out.UpdateTime
		*out = (*in).DeepCopy()
	}
	if in.Data != nil {
		in, out := &in.Data, &out.Data
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ExtFeatureStatus.
func (in *ExtFeatureStatus) DeepCopy() *ExtFeatureStatus {
	if in == nil {
		return nil
	}
	out := new(ExtFeatureStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *HealthAddressingSpec) DeepCopyInto(out *HealthAddressingSpec) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new HealthAddressingSpec.
func (in *HealthAddressingSpec) DeepCopy() *HealthAddressingSpec {
	if in == nil {
		return nil
	}
	out := new(HealthAddressingSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IPAllocation) DeepCopyInto(out *IPAllocation) {
	*out = *in
	in.DirectIPAllocation.DeepCopyInto(&out.DirectIPAllocation)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IPAllocation.
func (in *IPAllocation) DeepCopy() *IPAllocation {
	if in == nil {
		return nil
	}
	out := new(IPAllocation)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *IPAllocationStrategy) DeepCopyInto(out *IPAllocationStrategy) {
	*out = *in
	if in.TTL != nil {
		in, out := &in.TTL, &out.TTL
		*out = new(metav1.Duration)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new IPAllocationStrategy.
func (in *IPAllocationStrategy) DeepCopy() *IPAllocationStrategy {
	if in == nil {
		return nil
	}
	out := new(IPAllocationStrategy)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetResourceSet) DeepCopyInto(out *NetResourceSet) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetResourceSet.
func (in *NetResourceSet) DeepCopy() *NetResourceSet {
	if in == nil {
		return nil
	}
	out := new(NetResourceSet)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NetResourceSet) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetResourceSetList) DeepCopyInto(out *NetResourceSetList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]NetResourceSet, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetResourceSetList.
func (in *NetResourceSetList) DeepCopy() *NetResourceSetList {
	if in == nil {
		return nil
	}
	out := new(NetResourceSetList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *NetResourceSetList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetResourceSpec) DeepCopyInto(out *NetResourceSpec) {
	*out = *in
	if in.Addresses != nil {
		in, out := &in.Addresses, &out.Addresses
		*out = make([]NodeAddress, len(*in))
		copy(*out, *in)
	}
	in.IPAM.DeepCopyInto(&out.IPAM)
	if in.ENI != nil {
		in, out := &in.ENI, &out.ENI
		*out = new(api.ENISpec)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetResourceSpec.
func (in *NetResourceSpec) DeepCopy() *NetResourceSpec {
	if in == nil {
		return nil
	}
	out := new(NetResourceSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NetResourceStatus) DeepCopyInto(out *NetResourceStatus) {
	*out = *in
	in.IPAM.DeepCopyInto(&out.IPAM)
	if in.ENIs != nil {
		in, out := &in.ENIs, &out.ENIs
		*out = make(map[string]SimpleENIStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	if in.PrivateCloudSubnet != nil {
		in, out := &in.PrivateCloudSubnet, &out.PrivateCloudSubnet
		*out = new(privatecloudbaseapi.Subnet)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NetResourceStatus.
func (in *NetResourceStatus) DeepCopy() *NetResourceStatus {
	if in == nil {
		return nil
	}
	out := new(NetResourceStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *NodeAddress) DeepCopyInto(out *NodeAddress) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new NodeAddress.
func (in *NodeAddress) DeepCopy() *NodeAddress {
	if in == nil {
		return nil
	}
	out := new(NodeAddress)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ObjectReference) DeepCopyInto(out *ObjectReference) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ObjectReference.
func (in *ObjectReference) DeepCopy() *ObjectReference {
	if in == nil {
		return nil
	}
	out := new(ObjectReference)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodSubnetTopologySpread) DeepCopyInto(out *PodSubnetTopologySpread) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodSubnetTopologySpread.
func (in *PodSubnetTopologySpread) DeepCopy() *PodSubnetTopologySpread {
	if in == nil {
		return nil
	}
	out := new(PodSubnetTopologySpread)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PodSubnetTopologySpread) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodSubnetTopologySpreadList) DeepCopyInto(out *PodSubnetTopologySpreadList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]PodSubnetTopologySpread, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodSubnetTopologySpreadList.
func (in *PodSubnetTopologySpreadList) DeepCopy() *PodSubnetTopologySpreadList {
	if in == nil {
		return nil
	}
	out := new(PodSubnetTopologySpreadList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *PodSubnetTopologySpreadList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodSubnetTopologySpreadSpec) DeepCopyInto(out *PodSubnetTopologySpreadSpec) {
	*out = *in
	if in.Subnets != nil {
		in, out := &in.Subnets, &out.Subnets
		*out = make(map[string]CustomAllocationList, len(*in))
		for key, val := range *in {
			var outVal []CustomAllocation
			if val == nil {
				(*out)[key] = nil
			} else {
				in, out := &val, &outVal
				*out = make(CustomAllocationList, len(*in))
				for i := range *in {
					(*in)[i].DeepCopyInto(&(*out)[i])
				}
			}
			(*out)[key] = outVal
		}
	}
	if in.Strategy != nil {
		in, out := &in.Strategy, &out.Strategy
		*out = new(IPAllocationStrategy)
		(*in).DeepCopyInto(*out)
	}
	if in.Selector != nil {
		in, out := &in.Selector, &out.Selector
		*out = new(metav1.LabelSelector)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodSubnetTopologySpreadSpec.
func (in *PodSubnetTopologySpreadSpec) DeepCopy() *PodSubnetTopologySpreadSpec {
	if in == nil {
		return nil
	}
	out := new(PodSubnetTopologySpreadSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PodSubnetTopologySpreadStatus) DeepCopyInto(out *PodSubnetTopologySpreadStatus) {
	*out = *in
	if in.AvailableSubnets != nil {
		in, out := &in.AvailableSubnets, &out.AvailableSubnets
		*out = make(map[string]SubnetPodStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	if in.UnavailableSubnets != nil {
		in, out := &in.UnavailableSubnets, &out.UnavailableSubnets
		*out = make(map[string]SubnetPodStatus, len(*in))
		for key, val := range *in {
			(*out)[key] = *val.DeepCopy()
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PodSubnetTopologySpreadStatus.
func (in *PodSubnetTopologySpreadStatus) DeepCopy() *PodSubnetTopologySpreadStatus {
	if in == nil {
		return nil
	}
	out := new(PodSubnetTopologySpreadStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SimpleENIStatus) DeepCopyInto(out *SimpleENIStatus) {
	*out = *in
	if in.LastAllocatedIPError != nil {
		in, out := &in.LastAllocatedIPError, &out.LastAllocatedIPError
		*out = new(StatusChange)
		(*in).DeepCopyInto(*out)
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SimpleENIStatus.
func (in *SimpleENIStatus) DeepCopy() *SimpleENIStatus {
	if in == nil {
		return nil
	}
	out := new(SimpleENIStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *StatusChange) DeepCopyInto(out *StatusChange) {
	*out = *in
	in.Time.DeepCopyInto(&out.Time)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new StatusChange.
func (in *StatusChange) DeepCopy() *StatusChange {
	if in == nil {
		return nil
	}
	out := new(StatusChange)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubenetDetail) DeepCopyInto(out *SubenetDetail) {
	*out = *in
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubenetDetail.
func (in *SubenetDetail) DeepCopy() *SubenetDetail {
	if in == nil {
		return nil
	}
	out := new(SubenetDetail)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *SubnetPodStatus) DeepCopyInto(out *SubnetPodStatus) {
	*out = *in
	out.SubenetDetail = in.SubenetDetail
	if in.IPAllocations != nil {
		in, out := &in.IPAllocations, &out.IPAllocations
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new SubnetPodStatus.
func (in *SubnetPodStatus) DeepCopy() *SubnetPodStatus {
	if in == nil {
		return nil
	}
	out := new(SubnetPodStatus)
	in.DeepCopyInto(out)
	return out
}
