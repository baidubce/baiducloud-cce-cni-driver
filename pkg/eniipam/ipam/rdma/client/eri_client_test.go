package client

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	enisdk "github.com/baidubce/bce-sdk-go/services/eni"
)

func mockEriClient(t *testing.T) *EriClient {
	ctrl := gomock.NewController(t)

	return NewEriClient(mockcloud.NewMockInterface(ctrl))
}

func TestListEris(t *testing.T) {
	client := mockEriClient(t)
	ctx := context.Background()
	instanceID := "test-inst"

	client.cloud.(*mockcloud.MockInterface).EXPECT().ListERIs(gomock.Any(), gomock.Any()).Return([]enisdk.Eni{
		{
			PrivateIpSet: []enisdk.PrivateIp{
				{
					Primary:          false,
					PrivateIpAddress: "1.1.1.1",
				},
				{
					Primary:          false,
					PrivateIpAddress: "2.2.2.2",
				},
			},
		},
	}, nil)

	resultList, err := client.ListEnis(ctx, "", instanceID)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(resultList))
	assert.Equal(t, 2, len(resultList[0].PrivateIPSet))
}
