package k8s

import (
	"testing"

	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/stretchr/testify/assert"
)

func TestMatchResourceType(t *testing.T) {
	tests := []struct {
		name          string
		annotations   map[string]string
		networkType   string
		expectedMatch bool
	}{
		{
			name:          "empty annotations",
			annotations:   map[string]string{},
			networkType:   ccev2.NetResourceSetEventHandlerTypeEth,
			expectedMatch: true,
		},
		{
			name:          "empty annotations, expected rdma",
			annotations:   map[string]string{},
			networkType:   ccev2.NetResourceSetEventHandlerTypeRDMA,
			expectedMatch: false,
		},
		{
			name:          "rdma-vif-features present, expected eth",
			annotations:   map[string]string{AnnotationRDMAInfoVifFeatures: "true"},
			networkType:   ccev2.NetResourceSetEventHandlerTypeEth,
			expectedMatch: false,
		},
		{
			name:          "rdma-vif-features present, expected rdma",
			annotations:   map[string]string{AnnotationRDMAInfoVifFeatures: "true"},
			networkType:   ccev2.NetResourceSetEventHandlerTypeRDMA,
			expectedMatch: true,
		},
		{
			name:          "rdma-vif-features absent, expected eth",
			annotations:   map[string]string{},
			networkType:   ccev2.NetResourceSetEventHandlerTypeEth,
			expectedMatch: true,
		},
		{
			name:          "rdma-vif-features absent, expected rdma",
			annotations:   map[string]string{},
			networkType:   ccev2.NetResourceSetEventHandlerTypeRDMA,
			expectedMatch: false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			match := MatchNetworkResourceType(test.annotations, test.networkType)
			assert.Equal(t, test.expectedMatch, match)
		})
	}
}
