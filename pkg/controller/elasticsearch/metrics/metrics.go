package metrics

import (
	"context"

	esv1 "github.com/elastic/cloud-on-k8s/v2/pkg/apis/elasticsearch/v1"
	ulog "github.com/elastic/cloud-on-k8s/v2/pkg/utils/log"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/metrics"
)

func ReportMetrics(ctx context.Context, es *esv1.Elasticsearch) {
	if es == nil {
		return
	}
	log := ulog.FromContext(ctx)
	colors := []string{"green", "yellow", "red"}
	for _, color := range colors {
		log.Info("metrics: comparing colors", "color", color, "es_health", string(es.Status.Health))
		if string(es.Status.Health) == color {
			metrics.ElasticsearchState.WithLabelValues(es.GetName(), es.GetNamespace(), color).Set(1)
			continue
		}
		metrics.ElasticsearchState.WithLabelValues(es.GetName(), es.GetNamespace(), color).Set(0)
	}
	phases := []string{"Ready", "ApplyingChanges", "MigratingData", "Stalled", "Invalid"}
	for _, phase := range phases {
		log.Info("metrics: comparing phases", "phase", phase, "es_phase", string(es.Status.Phase))
		if string(es.Status.Phase) == phase {
			metrics.ElasticsearchPhase.WithLabelValues(es.GetName(), es.GetNamespace(), phase).Set(1)
			continue
		}
		metrics.ElasticsearchPhase.WithLabelValues(es.GetName(), es.GetNamespace(), phase).Set(0)
	}
}
