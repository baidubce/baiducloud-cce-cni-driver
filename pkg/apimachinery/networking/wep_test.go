package networking

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingv1alpha1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/apis/networking/v1alpha1"
	ipamgeneric "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/ipam"
	"github.com/baidubce/baiducloud-cce-cni-driver/test/data"
)

func TestISCustomReuseModeWEP(t *testing.T) {
	wep := data.MockFixedWorkloadEndpoint()
	if !IsFixIPStatefulSetPodWep(wep) {
		t.Fail()
	}
	wep.Spec.Type = ipamgeneric.WepTypeReuseIPPod
	if !ISCustomReuseModeWEP(wep) {
		t.Fail()
	}
	NeedReleaseReuseModeWEP(wep)
	now := metav1.Now()
	wep.Spec.Phase = networkingv1alpha1.WorkloadEndpointPhasePodDeleted
	wep.Spec.Release = &networkingv1alpha1.EndpointRelease{
		TTL:            metav1.Duration{Duration: time.Second},
		PodDeletedTime: &now,
	}

	NeedReleaseReuseModeWEP(wep)
}
