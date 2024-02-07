package client

import (
	"context"
	"testing"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"

	mockcloud "github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/cloud/testing"
	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/bce/hpc"
)

func mockRoCEClient(t *testing.T) *RoCEClient {
	ctrl := gomock.NewController(t)

	return NewRoCEClient(mockcloud.NewMockInterface(ctrl))
}

func TestListHpcEnis(t *testing.T) {
	client := mockRoCEClient(t)
	ctx := context.Background()
	instanceID := "test-inst"

	client.cloud.(*mockcloud.MockInterface).EXPECT().GetHPCEniID(gomock.Any(), instanceID).Return(&hpc.EniList{
		Result: []hpc.Result{
			{
				PrivateIPSet: []hpc.PrivateIP{
					{
						Primary:          false,
						PrivateIPAddress: "1.1.1.1",
					},
					{
						Primary:          false,
						PrivateIPAddress: "2.2.2.2",
					},
				},
			},
		},
	}, nil)

	resultList, err := client.ListEnis(ctx, "", instanceID)
	assert.Nil(t, err)
	assert.Equal(t, 1, len(resultList))
	assert.Equal(t, 2, len(resultList[0].PrivateIPSet))
}

func TestAddPrivateIP(t *testing.T) {
	client := mockRoCEClient(t)
	ctx := context.Background()
	eniID := "test-eni"

	client.cloud.(*mockcloud.MockInterface).EXPECT().BatchAddHpcEniPrivateIP(gomock.Any(), gomock.Any()).
		Return(&hpc.BatchAddPrivateIPResult{
			PrivateIPAddresses: []string{"1.1.1.1"},
		}, nil)

	ipResult, err := client.AddPrivateIP(ctx, eniID, "")
	assert.Nil(t, err)
	assert.Equal(t, "1.1.1.1", ipResult)
}

func TestDeletePrivateIP(t *testing.T) {
	client := mockRoCEClient(t)
	ctx := context.Background()
	eniID := "test-eni"

	client.cloud.(*mockcloud.MockInterface).EXPECT().BatchDeleteHpcEniPrivateIP(gomock.Any(), gomock.Any()).Return(nil)

	err := client.DeletePrivateIP(ctx, eniID, "1.1.1.1")
	assert.Nil(t, err)
}
