package vpceni

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/stretchr/testify/assert"
)

func Test_getDefaultBCCEniQuota(t *testing.T) {
	maxEni, maxIP := getDefaultBCCEniQuota(&corev1.Node{
		Status: corev1.NodeStatus{
			Capacity: corev1.ResourceList{
				corev1.ResourceCPU:    *resource.NewScaledQuantity(10000, resource.Milli),
				corev1.ResourceMemory: *resource.NewQuantity(6*1024*1024*1024, resource.BinarySI),
			},
		},
	})
	assert.Equal(t, 8, maxEni)
	assert.Equal(t, 8, maxIP)
}
