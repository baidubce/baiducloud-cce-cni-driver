package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/operator/option"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/logging"
	"github.com/baidubce/baiducloud-cce-cni-driver/cce-network-v2/pkg/metrics"
)

var (
	log = logging.NewSubysLogger("bce-cloud")
)

func exportMetric(funcName string, startTime time.Time, err error) {
	metrics.CloudAPIRequestDurationMillisesconds.WithLabelValues(
		option.Config.CCEClusterID,
		funcName,
		fmt.Sprint(err != nil),
		fmt.Sprint(BceServiceErrorToHTTPCode(err)),
	).Observe(float64(time.Since(startTime) / time.Millisecond))
}

func exportMetricAndLog(ctx context.Context, funcName string, startTime time.Time, err error) {
	exportMetric(funcName, startTime, err)
	log.WithContext(ctx).WithField("funcName", funcName).WithField("elapsed", time.Since(startTime)).Info("export metric success")
}
