package nodediscovery

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/record"

	bceapi "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/bce/api/metadata"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s"
	ccev2 "github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/k8s/apis/cce.baidubce.com/v2"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/option"
)

type fakeMetadataClient struct {
	metadata.Interface
	getInstanceSpecs  func() (string, error)
	getVPCID          func() (string, error)
	getInstanceTypeEx func() (metadata.InstanceTypeEx, error)
	getSourceType     func() (string, error)
	getInstanceType   func() (metadata.InstanceType, error)
	getAZ             func() (string, error)
}

func (f *fakeMetadataClient) GetInstanceSpecs() (string, error) {
	return f.getInstanceSpecs()
}

func (f *fakeMetadataClient) GetVPCID() (string, error) {
	if f.getVPCID != nil {
		return f.getVPCID()
	}
	return "vpc-test", nil
}

func (f *fakeMetadataClient) GetInstanceTypeEx() (metadata.InstanceTypeEx, error) {
	if f.getInstanceTypeEx != nil {
		return f.getInstanceTypeEx()
	}
	return metadata.InstanceTypeExBCC, nil
}

func (f *fakeMetadataClient) GetSourceType() (string, error) {
	if f.getSourceType != nil {
		return f.getSourceType()
	}
	return "", nil
}

func (f *fakeMetadataClient) GetInstanceType() (metadata.InstanceType, error) {
	if f.getInstanceType != nil {
		return f.getInstanceType()
	}
	return metadata.InstanceType("bcc.g1.c2m4"), nil
}

func (f *fakeMetadataClient) GetAvailabilityZone() (string, error) {
	if f.getAZ != nil {
		return f.getAZ()
	}
	return "cn-bj-a", nil
}

func TestRefreshVpcENIConfiguration_ForcePrimaryENIError(t *testing.T) {
	oldENI := option.Config.ENI
	oldSpecs := option.Config.ForcePrimaryENIInstanceSpecs
	oldMetaClient := metadata.DefaultMetaClient
	t.Cleanup(func() {
		option.Config.ENI = oldENI
		option.Config.ForcePrimaryENIInstanceSpecs = oldSpecs
		metadata.DefaultMetaClient = oldMetaClient
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

	metadata.DefaultMetaClient = &fakeMetadataClient{
		Interface: oldMetaClient,
		getInstanceSpecs: func() (string, error) {
			return "", errors.New("metadata error")
		},
	}

	nd.refreshVpcENIConfiguration(nodeResource, nil, k8sNode)

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
	oldMetaClient := metadata.DefaultMetaClient
	t.Cleanup(func() {
		option.Config.ENI = oldENI
		option.Config.ForcePrimaryENIInstanceSpecs = oldSpecs
		metadata.DefaultMetaClient = oldMetaClient
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

	metadata.DefaultMetaClient = &fakeMetadataClient{
		Interface: oldMetaClient,
		getInstanceSpecs: func() (string, error) {
			return "bcc.g1.c2m4", nil
		},
	}

	nd.refreshVpcENIConfiguration(nodeResource, nil, k8sNode)

	if assert.NotNil(t, nodeResource.Spec.ENI) {
		assert.Equal(t, string(ccev2.ENIUseModePrimaryWithSecondaryIP), nodeResource.Spec.ENI.UseMode)
	}
}
