package cloud

import (
	"context"
	"fmt"
	"time"

	"github.com/baidubce/baiducloud-cce-cni-driver/pkg/metric"
	log "github.com/baidubce/baiducloud-cce-cni-driver/pkg/util/logger"
)

func exportMetric(funcName string, startTime time.Time, err error) {
	metric.OpenAPILatency.WithLabelValues(
		metric.MetaInfo.ClusterID,
		funcName,
		fmt.Sprint(err != nil),
		fmt.Sprint(BceServiceErrorToHTTPCode(err)),
	).Observe(metric.MsSince(startTime))
}

func exportMetricAndLog(ctx context.Context, funcName string, startTime time.Time, err error) {
	exportMetric(funcName, startTime, err)
	log.Infof(ctx, "%s elapsed: %v", funcName, time.Since(startTime))
}
