package endpoint

import (
	"reflect"
	"testing"
	"time"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/require"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/api/v1/models"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUpdateSyncedPoolStaleSkip(t *testing.T) {
	manager := &EndpointManager{}
	manager.addToSyncedPool("ns/cep", "5")

	patches := gomonkey.NewPatches()
	defer patches.Reset()
	patches.ApplyPrivateMethod(reflect.TypeOf(&EndpointManager{}), "recordEvent", func(_ *EndpointManager, _ string, _ time.Time, _ error, _ *ccev2.CCEEndpoint) {})

	resource := &ccev2.CCEEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       "ns",
			Name:            "cep",
			ResourceVersion: "4",
		},
		Spec: ccev2.EndpointSpec{
			Network: ccev2.EndpointNetworkSpec{
				IPAllocation: &ccev2.IPAllocation{PSTSName: "psts"},
			},
		},
	}

	err := manager.Update(resource)
	require.NoError(t, err)
	require.True(t, manager.isInSyncedPool("ns/cep"))
}

func TestShouldSkipReuseUpdate_RestoringShouldNotSkip(t *testing.T) {
	resource := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "uid-1",
			},
		},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateRestoring),
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "uid-1",
			},
			Networking: &ccev2.EndpointNetworking{
				NodeIP: "node-a",
				Addressing: ccev2.AddressPairList{
					{IP: "10.0.0.2"},
				},
			},
		},
	}

	require.False(t, shouldSkipReuseUpdate(resource, "node-a"))
}

func TestShouldSkipReuseUpdate_ReadyAndMatchedShouldSkip(t *testing.T) {
	resource := &ccev2.CCEEndpoint{
		Spec: ccev2.EndpointSpec{
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "uid-1",
			},
		},
		Status: ccev2.EndpointStatus{
			State: string(models.EndpointStateIPAllocated),
			ExternalIdentifiers: &models.EndpointIdentifiers{
				K8sObjectID: "uid-1",
			},
			Networking: &ccev2.EndpointNetworking{
				NodeIP: "node-a",
				Addressing: ccev2.AddressPairList{
					{IP: "10.0.0.2"},
				},
			},
		},
	}

	require.True(t, shouldSkipReuseUpdate(resource, "node-a"))
}

func TestShouldSkipReuseUpdate_NilAndMismatchBranches(t *testing.T) {
	require.False(t, shouldSkipReuseUpdate(nil, "node-a"))

	tests := []struct {
		name     string
		resource *ccev2.CCEEndpoint
	}{
		{
			name: "missing spec external identifiers",
			resource: &ccev2.CCEEndpoint{
				Status: ccev2.EndpointStatus{
					State: string(models.EndpointStateIPAllocated),
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
					Networking: &ccev2.EndpointNetworking{
						NodeIP: "node-a",
						Addressing: ccev2.AddressPairList{
							{IP: "10.0.0.2"},
						},
					},
				},
			},
		},
		{
			name: "missing status external identifiers",
			resource: &ccev2.CCEEndpoint{
				Spec: ccev2.EndpointSpec{
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
				},
				Status: ccev2.EndpointStatus{
					State: string(models.EndpointStateIPAllocated),
					Networking: &ccev2.EndpointNetworking{
						NodeIP: "node-a",
						Addressing: ccev2.AddressPairList{
							{IP: "10.0.0.2"},
						},
					},
				},
			},
		},
		{
			name: "missing networking",
			resource: &ccev2.CCEEndpoint{
				Spec: ccev2.EndpointSpec{
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
				},
				Status: ccev2.EndpointStatus{
					State: string(models.EndpointStateIPAllocated),
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
				},
			},
		},
		{
			name: "node ip mismatch",
			resource: &ccev2.CCEEndpoint{
				Spec: ccev2.EndpointSpec{
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
				},
				Status: ccev2.EndpointStatus{
					State: string(models.EndpointStateIPAllocated),
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
					Networking: &ccev2.EndpointNetworking{
						NodeIP: "node-b",
						Addressing: ccev2.AddressPairList{
							{IP: "10.0.0.2"},
						},
					},
				},
			},
		},
		{
			name: "empty addressing",
			resource: &ccev2.CCEEndpoint{
				Spec: ccev2.EndpointSpec{
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
				},
				Status: ccev2.EndpointStatus{
					State: string(models.EndpointStateIPAllocated),
					ExternalIdentifiers: &models.EndpointIdentifiers{
						K8sObjectID: "uid-1",
					},
					Networking: &ccev2.EndpointNetworking{
						NodeIP:     "node-a",
						Addressing: ccev2.AddressPairList{},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.False(t, shouldSkipReuseUpdate(tt.resource, "node-a"))
		})
	}
}
