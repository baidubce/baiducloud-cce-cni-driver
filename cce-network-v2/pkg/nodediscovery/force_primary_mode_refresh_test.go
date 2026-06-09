package nodediscovery

import (
	"errors"
	"testing"

	"github.com/agiledragon/gomonkey/v2"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/agent"
	bceapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

func TestRefreshVpcENIConfiguration_ForcePrimaryENIError(t *testing.T) {
	oldENI := option.Config.ENI
	oldSpecs := option.Config.ForcePrimaryENIInstanceSpecs
	t.Cleanup(func() {
		option.Config.ENI = oldENI
		option.Config.ForcePrimaryENIInstanceSpecs = oldSpecs
	})

	option.Config.ENI = &bceapi.ENISpec{
		SubnetIDs: []string{"sbn-test"},
	}
	option.Config.ForcePrimaryENIInstanceSpecs = []string{"bcc.*"}

	recorder := record.NewFakeRecorder(10)
	nd := &NodeDiscovery{eventRecorder: recorder}
	nodeResource := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelENIUseMode: string(ccev2.ENIUseModeSecondaryIP),
			},
		},
		Spec: ccev2.NetResourceSpec{
			ENI: &bceapi.ENISpec{
				UseMode: string(ccev2.ENIUseModeSecondaryIP),
			},
		},
	}
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
	}

	called := false
	patches := gomonkey.ApplyFunc(agent.GetInstanceSpecs, func() (string, error) {
		called = true
		return "", errors.New("metadata error")
	})
	defer patches.Reset()

	nd.refreshVpcENIConfiguration(nodeResource, nil, k8sNode)

	assert.True(t, called)
	select {
	case event := <-recorder.Events:
		assert.Contains(t, event, "MetaAPIError")
		assert.Contains(t, event, "failed to get instance specs")
	default:
		t.Fatal("expected warning event for instance specs error")
	}
}

func TestRefreshVpcENIConfiguration_ForcePrimaryENISuccess(t *testing.T) {
	oldENI := option.Config.ENI
	oldSpecs := option.Config.ForcePrimaryENIInstanceSpecs
	t.Cleanup(func() {
		option.Config.ENI = oldENI
		option.Config.ForcePrimaryENIInstanceSpecs = oldSpecs
	})

	option.Config.ENI = &bceapi.ENISpec{
		SubnetIDs: []string{"sbn-test"},
	}
	option.Config.ForcePrimaryENIInstanceSpecs = []string{"bcc.*"}

	recorder := record.NewFakeRecorder(10)
	nd := &NodeDiscovery{eventRecorder: recorder}
	nodeResource := &ccev2.NetResourceSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-node",
			Labels: map[string]string{
				k8s.LabelENIUseMode: string(ccev2.ENIUseModeSecondaryIP),
			},
		},
	}
	k8sNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: "test-node"},
	}

	called := false
	patches := gomonkey.ApplyFunc(agent.GetInstanceSpecs, func() (string, error) {
		called = true
		return "bcc.g1.c2m4", nil
	})
	patches.ApplyFunc(agent.GenerateENISpec, func() (*bceapi.ENISpec, error) {
		return &bceapi.ENISpec{}, nil
	})
	defer patches.Reset()

	nd.refreshVpcENIConfiguration(nodeResource, nil, k8sNode)

	assert.True(t, called)
	if assert.NotNil(t, nodeResource.Spec.ENI) {
		assert.Equal(t, string(ccev2.ENIUseModePrimaryWithSecondaryIP), nodeResource.Spec.ENI.UseMode)
	}
}
