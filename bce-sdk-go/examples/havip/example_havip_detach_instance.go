package havipexample

import (
	"fmt"

	"github.com/baidubce/bce-sdk-go/services/havip"
)

func HaVipDetachInstance() {
	ak, sk, endpoint := "Your AK", "Your SK", "bcc.bj.baidubce.com"
	HAVIP_CLIENT, _ := havip.NewClient(ak, sk, endpoint) // 初始化client

	haVipInstanceArgs := &havip.HaVipInstanceArgs{
		haVipId:      "havip_id",                    // 高可用虚拟IP的ID
		instanceIds:  []string{"Your instance ids"}, // 绑定的实例ID列表，列表长度不大于5
		instanceType: "bcc",                         // 绑定的实例类型，"SERVER"表示云服务器（BCC/BBC/DCC），"ENI"表示弹性网卡
	}
	response, err := HAVIP_CLIENT.HaVipDetachhInstance(haVipInstanceArgs) // 高可用虚拟IP解绑实例

	if err != nil {
		panic(err)
	}
	fmt.Println(response)
}
