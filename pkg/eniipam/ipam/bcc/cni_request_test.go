package bcc

import (
	"context"
	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	datastorev1 "github.com/baidubce/baiducloud-cce-cni-driver/pkg/eniipam/datastore/v1"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestIPAM_ensureNodeInStore(t *testing.T) {
	ctrl := gomock.NewController(t)
	mockInterface := mockcloud.NewMockInterface(ctrl)
	mockInterface.EXPECT().ListENIs(gomock.Any(), gomock.Any()).Return([]enisdk.Eni{}, nil).AnyTimes()

	ipam := &IPAM{
		datastore:               datastorev1.NewDataStore(),
		buildDataStoreEventChan: make(map[string]chan *event),
		cloud:                   mockInterface,
	}
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "i-xxxx0",
		},
		Spec: v1.NodeSpec{
			ProviderID: "cce://i-xxxx0",
		},
	}
	err := ipam.ensureNodeInStore(context.Background(), node)
	assert.Nil(t, err)
}
