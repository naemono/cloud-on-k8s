// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	crmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	ElasticsearchState = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "eck_elasticsearch_health_status",
			Help: "Health of Elasticsearch cluster managed by ECK Operator",
		},
		[]string{"name", "namespace", "color"},
	)
	ElasticsearchPhase = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "eck_elasticsearch_phase",
			Help: "Phase of Elasticsearch cluster managed by ECK Operator",
		},
		[]string{"name", "namespace", "phase"},
	)
)

func init() {
	// register the prometheus collector with the controller runtime registry
	crmetrics.Registry.MustRegister(ElasticsearchState)
}
