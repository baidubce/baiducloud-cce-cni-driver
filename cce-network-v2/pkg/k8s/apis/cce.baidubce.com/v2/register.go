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

package v2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"

	k8sconst "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com"
)

const (
	// CustomResourceDefinitionGroup is the name of the third party resource group
	CustomResourceDefinitionGroup = k8sconst.CustomResourceDefinitionGroup

	// CustomResourceDefinitionVersion is the current version of the resource
	CustomResourceDefinitionVersion = "v2"

	// CustomResourceDefinitionSchemaVersion is semver-conformant version of CRD schema
	// Used to determine if CRD needs to be updated in cluster
	//
	// Maintainers: Run ./Documentation/check-crd-compat-table.sh for each release
	// Developers: Bump patch for each change in the CRD schema.
	CustomResourceDefinitionSchemaVersion = "1.25.4"

	// CustomResourceDefinitionSchemaVersionKey is key to label which holds the CRD schema version
	CustomResourceDefinitionSchemaVersionKey = "cce.baidubce.com.k8s.crd.schema.version"

	// CCE Endpoint (CEP)

	// CESingularName is the singular name of CCE Endpoint
	CEPSingularName = "cceendpoint"

	// CEPluralName is the plural name of CCE Endpoint
	CEPPluralName = "cceendpoints"

	// CEKindDefinition is the kind name for CCE Endpoint
	CEPKindDefinition = "CCEEndpoint"

	// CEPName is the full name of CCE Endpoint
	CEPName = CEPPluralName + "." + CustomResourceDefinitionGroup

	// CCE Node (CN)

	// NRSSingularName is the singular name of CCE Node
	NRSSingularName = "netresourceset"

	// NRSPluralName is the plural name of CCE Node
	NRSPluralName = "netresourcesets"

	// NRSKindDefinition is the kind name for CCE Node
	NRSKindDefinition = "NetResourceSet"

	// NRSName is the full name of CCE Node
	NRSName = NRSPluralName + "." + CustomResourceDefinitionGroup

	// ENI

	// ENISingularName is the singular name of ENI
	ENISingularName = "eni"

	// CNPluralName is the plural name of CCE Node
	ENIPluralName = "enis"

	// ENIKindDefinition is the kind name for CCE Node
	ENIKindDefinition = "ENI"

	// ENIName is the full name of CCE Node
	ENIName = ENIPluralName + "." + CustomResourceDefinitionGroup

	PSTSName = "podsubnettopologyspreads" + "." + CustomResourceDefinitionGroup
)

// SchemeGroupVersion is group version used to register these objects
var SchemeGroupVersion = schema.GroupVersion{
	Group:   CustomResourceDefinitionGroup,
	Version: CustomResourceDefinitionVersion,
}

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}

var (
	// SchemeBuilder is needed by DeepCopy generator.
	SchemeBuilder runtime.SchemeBuilder
	// localSchemeBuilder and AddToScheme will stay in k8s.io/kubernetes.
	localSchemeBuilder = &SchemeBuilder

	// AddToScheme adds all types of this clientset into the given scheme.
	// This allows composition of clientsets, like in:
	//
	//   import (
	//     "k8s.io/client-go/kubernetes"
	//     clientsetscheme "k8s.io/client-go/kubernetes/scheme"
	//     aggregatorclientsetscheme "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/scheme"
	//   )
	//
	//   kclientset, _ := kubernetes.NewForConfig(c)
	//   aggregatorclientsetscheme.AddToScheme(clientsetscheme.Scheme)
	AddToScheme = localSchemeBuilder.AddToScheme
)

func init() {
	// We only register manually written functions here. The registration of the
	// generated functions takes place in the generated files. The separation
	// makes the code compile even when the generated files are missing.
	localSchemeBuilder.Register(addKnownTypes)
}

// Adds the list of known types to api.Scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&NetResourceSet{},
		&NetResourceSetList{},
		&CCEEndpoint{},
		&CCEEndpointList{},
		&ENI{},
		&ENIList{},
		&PodSubnetTopologySpread{},
		&PodSubnetTopologySpreadList{},
	)

	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
