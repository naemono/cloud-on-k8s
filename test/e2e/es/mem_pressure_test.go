// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License 2.0;
// you may not use this file except in compliance with the Elastic License 2.0.

//go:build es || e2e

package es

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	esv1 "github.com/elastic/cloud-on-k8s/v3/pkg/apis/elasticsearch/v1"
	esclient "github.com/elastic/cloud-on-k8s/v3/pkg/controller/elasticsearch/client"
	"github.com/elastic/cloud-on-k8s/v3/pkg/controller/elasticsearch/label"
	"github.com/elastic/cloud-on-k8s/v3/pkg/utils/k8s"
	"github.com/elastic/cloud-on-k8s/v3/test/e2e/test"
	"github.com/elastic/cloud-on-k8s/v3/test/e2e/test/elasticsearch"
)

const (
	// ~25k indices * 1 primary * (1+1 replica) = ~50k total shards.
	// Enough to make _cat/shards responses several MBs.
	numIndicesToCreate = 25000
	shardsPerIndex     = 1
	replicasPerIndex   = 1

	memSampleInterval = 5 * time.Second
	// How long to watch memory once the disruption+mutation is active.
	memWatchDuration = 3 * time.Minute
)

// memSample is a single operator memory observation.
type memSample struct {
	Time     time.Time
	Bytes    int64
	Restarts int32
}

// memWatcher periodically records the memory usage of the ECK operator pod
// via the Kubernetes metrics API (metrics.k8s.io).
type memWatcher struct {
	mu       sync.Mutex
	samples  []memSample
	stop     chan struct{}
	clientGo kubernetes.Interface
	podName  string
	podNs    string
}

func newMemWatcher(clientGo kubernetes.Interface, podName, podNs string) *memWatcher {
	return &memWatcher{
		clientGo: clientGo,
		podName:  podName,
		podNs:    podNs,
		stop:     make(chan struct{}),
	}
}

func (w *memWatcher) Start() {
	go func() {
		ticker := time.NewTicker(memSampleInterval)
		defer ticker.Stop()
		for {
			select {
			case <-w.stop:
				return
			case <-ticker.C:
				w.recordSample()
			}
		}
	}()
}

func (w *memWatcher) Stop() {
	close(w.stop)
}

// podMetricsResult mirrors the relevant subset of metrics.k8s.io PodMetrics.
type podMetricsResult struct {
	Containers []struct {
		Name  string            `json:"name"`
		Usage map[string]string `json:"usage"`
	} `json:"containers"`
}

func (w *memWatcher) recordSample() {
	// Fetch pod to get container restart count.
	pod, err := w.clientGo.CoreV1().Pods(w.podNs).Get(context.Background(), w.podName, metav1.GetOptions{})
	if err != nil {
		return
	}
	var restarts int32
	for _, cs := range pod.Status.ContainerStatuses {
		if cs.Name == "manager" {
			restarts = cs.RestartCount
		}
	}

	// Query the metrics API for actual memory usage.
	raw, err := w.clientGo.CoreV1().RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1").
		Namespace(w.podNs).
		Resource("pods").
		Name(w.podName).
		DoRaw(context.Background())
	if err != nil {
		// Metrics API unavailable; record sentinel.
		w.mu.Lock()
		w.samples = append(w.samples, memSample{Time: time.Now(), Bytes: -1, Restarts: restarts})
		w.mu.Unlock()
		return
	}

	var pm podMetricsResult
	if err := json.Unmarshal(raw, &pm); err != nil {
		return
	}
	for _, c := range pm.Containers {
		if c.Name != "manager" {
			continue
		}
		memStr, ok := c.Usage["memory"]
		if !ok {
			continue
		}
		memQty, err := resource.ParseQuantity(memStr)
		if err != nil {
			continue
		}
		w.mu.Lock()
		w.samples = append(w.samples, memSample{Time: time.Now(), Bytes: memQty.Value(), Restarts: restarts})
		w.mu.Unlock()
	}
}

func (w *memWatcher) Samples() []memSample {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]memSample, len(w.samples))
	copy(out, w.samples)
	return out
}

func (w *memWatcher) PeakBytes() int64 {
	var peak int64
	for _, s := range w.Samples() {
		if s.Bytes > peak {
			peak = s.Bytes
		}
	}
	return peak
}

// TestOperatorMemoryUnderShardPressure verifies whether operator memory spikes
// when the cluster enters a yellow-health / "Disruptions not allowed" requeue
// loop with a large number of shards.
//
// Hypothesis: constant requeue with many shards causes GetShards to be called
// repeatedly in upgrade predicates, each time allocating a large []Shard
// response that pressures GC and causes 100s-of-MiB memory growth.
//
// Phases:
//  1. Create ES cluster (3 masters + 2 data nodes)
//  2. Bulk-create ~25k indices (1p/1r) → ~50k total shards
//  3. Start monitoring operator memory (baseline, ~30s)
//  4. Delete one data pod → yellow health → "Disruptions not allowed" for data
//  5. Apply a trivial spec mutation → pods need upgrade → predicates fire
//  6. Observe operator memory for 3 min during constant requeue
//  7. Compare peak-during-pressure vs. baseline and report
func TestOperatorMemoryUnderShardPressure(t *testing.T) {
	esResources := corev1.ResourceRequirements{
		Limits: map[corev1.ResourceName]resource.Quantity{
			corev1.ResourceMemory: resource.MustParse("4Gi"),
			corev1.ResourceCPU:    resource.MustParse("2"),
		},
	}
	b := elasticsearch.NewBuilder("test-mem-shard-pressure").
		WithESMasterNodes(3, elasticsearch.DefaultResources).
		WithNamedESDataNodes(2, "data", esResources)

	k := test.NewK8sClientOrFatal()

	steps := b.InitTestSteps(k)
	steps = steps.WithSteps(b.CreationTestSteps(k))
	steps = steps.WithSteps(test.CheckTestSteps(b, k))

	// Phase 2: Load shards.
	steps = steps.WithStep(test.Step{
		Name: "Create ~25k indices to produce ~50k shards",
		Test: test.Eventually(func() error {
			return createPressureIndices(b.Elasticsearch, k)
		}),
	})
	steps = steps.WithStep(test.Step{
		Name: "Verify shard count is in expected range",
		Test: test.Eventually(func() error {
			return verifyShardCount(b.Elasticsearch, k)
		}),
	})

	// Phase 3: Start memory watcher and collect baseline.
	var watcher *memWatcher
	steps = steps.WithStep(test.Step{
		Name: "Start operator memory watcher and collect 30s baseline",
		Test: func(t *testing.T) {
			clientGo, err := buildGoClient()
			require.NoError(t, err)
			watcher = newMemWatcher(
				clientGo,
				test.Ctx().Operator.Name+"-0",
				test.Ctx().Operator.Namespace,
			)
			watcher.Start()
			t.Logf("Memory watcher started; collecting baseline for 30s")
			time.Sleep(30 * time.Second)
			peak := watcher.PeakBytes()
			if peak > 0 {
				t.Logf("Baseline peak: %.1f MiB", float64(peak)/(1024*1024))
			} else {
				t.Log("Baseline: metrics API unavailable, will report sample counts only")
			}
		},
	})

	// Phase 4: Delete one data pod to induce yellow health.
	steps = steps.WithStep(test.Step{
		Name: "Delete a data pod to force yellow health",
		Test: func(t *testing.T) {
			pods, err := k.GetPods(test.ESPodListOptions(b.Elasticsearch.Namespace, b.Elasticsearch.Name)...)
			require.NoError(t, err)
			for _, pod := range pods {
				if label.IsDataNode(pod) && !label.IsMasterNode(pod) {
					t.Logf("Deleting data pod %s", pod.Name)
					require.NoError(t, k.DeletePod(pod))
					return
				}
			}
			t.Fatal("No data pod found to delete")
		},
	})

	// Phase 5: Apply a trivial spec change so data pods need a rolling upgrade.
	// In yellow health the upgrade predicates will fire but block healthy pods,
	// and GetShards will be called for any unhealthy pod candidate.
	steps = steps.WithStep(test.Step{
		Name: "Apply trivial spec change to trigger rolling upgrade predicates",
		Test: test.Eventually(func() error {
			var es esv1.Elasticsearch
			if err := k.Client.Get(context.Background(), k8s.ExtractNamespacedName(&b.Elasticsearch), &es); err != nil {
				return err
			}
			for i := range es.Spec.NodeSets {
				if es.Spec.NodeSets[i].Name != "data" {
					continue
				}
				if es.Spec.NodeSets[i].Config == nil {
					continue
				}
				es.Spec.NodeSets[i].Config.Data["node.attr.pressure_test"] = "true"
			}
			return k.Client.Update(context.Background(), &es)
		}),
	})

	// Phase 6: Observe memory during the constant-requeue period.
	steps = steps.WithStep(test.Step{
		Name: fmt.Sprintf("Observe operator memory for %s during constant requeue", memWatchDuration),
		Test: func(t *testing.T) {
			t.Logf("Waiting %s while the operator is in the Disruptions-not-allowed requeue loop...", memWatchDuration)
			time.Sleep(memWatchDuration)
		},
	})

	// Phase 7: Analyze results.
	steps = steps.WithStep(test.Step{
		Name: "Analyze operator memory samples",
		Test: func(t *testing.T) {
			watcher.Stop()
			samples := watcher.Samples()
			t.Logf("Collected %d memory samples total", len(samples))
			if len(samples) == 0 {
				t.Log("WARNING: no samples collected; metrics API may be unavailable")
				return
			}

			// Split into baseline (first ~6 samples, i.e. 30s) and pressure (rest).
			const baselineSamples = 6
			var baselinePeak, pressurePeak int64
			var metricsAvailable bool

			for i, s := range samples {
				if s.Bytes < 0 {
					continue
				}
				metricsAvailable = true
				if i < baselineSamples {
					if s.Bytes > baselinePeak {
						baselinePeak = s.Bytes
					}
				} else {
					if s.Bytes > pressurePeak {
						pressurePeak = s.Bytes
					}
				}
			}

			if !metricsAvailable {
				t.Log("Metrics API was unavailable; cannot compare memory. Reproduce with `kubectl top pod` manually.")
				return
			}

			bMiB := float64(baselinePeak) / (1024 * 1024)
			pMiB := float64(pressurePeak) / (1024 * 1024)
			dMiB := pMiB - bMiB

			t.Logf("Baseline peak: %.1f MiB", bMiB)
			t.Logf("Pressure peak: %.1f MiB", pMiB)
			t.Logf("Delta:         %.1f MiB", dMiB)

			for _, s := range samples {
				t.Logf("  %s  mem=%.1fMiB  restarts=%d",
					s.Time.Format("15:04:05"), float64(s.Bytes)/(1024*1024), s.Restarts)
			}

			// The hypothesis predicts 100-200+ MiB growth from repeated
			// GetShards allocations during the predicate-evaluation requeue loop.
			const thresholdMiB = 100.0
			if dMiB > thresholdMiB {
				t.Errorf("Operator memory grew by %.1f MiB (baseline=%.1f, pressure=%.1f), "+
					"exceeding %.0f MiB threshold. This supports the hypothesis that "+
					"repeated un-cached GetShards calls in upgrade predicates cause "+
					"significant memory pressure with many shards.",
					dMiB, bMiB, pMiB, thresholdMiB)
			} else {
				t.Logf("Delta %.1f MiB is within %.0f MiB threshold", dMiB, thresholdMiB)
			}
		},
	})

	// Cleanup: revert the spec change so the cluster can recover.
	steps = steps.WithStep(test.Step{
		Name: "Revert spec change to allow cluster recovery",
		Test: test.Eventually(func() error {
			var es esv1.Elasticsearch
			if err := k.Client.Get(context.Background(), k8s.ExtractNamespacedName(&b.Elasticsearch), &es); err != nil {
				return err
			}
			for i := range es.Spec.NodeSets {
				if es.Spec.NodeSets[i].Config != nil {
					delete(es.Spec.NodeSets[i].Config.Data, "node.attr.pressure_test")
				}
			}
			return k.Client.Update(context.Background(), &es)
		}),
	})

	steps = steps.WithSteps(test.CheckTestSteps(b, k))
	steps = steps.WithSteps(b.DeletionTestSteps(k))

	steps.RunSequential(t)
}

// createPressureIndices bulk-creates indices named pressure-idx-00000 through
// pressure-idx-NNNNN. Each has 1 primary and 1 replica shard. It resumes from
// however many already exist.
func createPressureIndices(es esv1.Elasticsearch, k *test.K8sClient) error {
	esClient, err := elasticsearch.NewElasticsearchClient(es, k)
	if err != nil {
		return err
	}
	defer esClient.Close()

	existing, err := countPressureIndices(esClient)
	if err != nil {
		return fmt.Errorf("counting existing indices: %w", err)
	}
	if existing >= numIndicesToCreate {
		return nil
	}

	settingsJSON := fmt.Sprintf(
		`{"settings":{"number_of_shards":%d,"number_of_replicas":%d}}`,
		shardsPerIndex, replicasPerIndex,
	)

	batchSize := 500
	for i := existing; i < numIndicesToCreate; i += batchSize {
		end := i + batchSize
		if end > numIndicesToCreate {
			end = numIndicesToCreate
		}
		for j := i; j < end; j++ {
			indexName := fmt.Sprintf("pressure-idx-%05d", j)
			req, err := http.NewRequest( //nolint:noctx
				http.MethodPut,
				"/"+indexName,
				bytes.NewBufferString(settingsJSON),
			)
			if err != nil {
				return err
			}
			resp, err := esClient.Request(context.Background(), req)
			if resp != nil {
				resp.Body.Close()
			}
			if err != nil {
				// resource_already_exists_exception is fine.
				if isAlreadyExists(err) {
					continue
				}
				return fmt.Errorf("creating index %s: %w", indexName, err)
			}
		}
	}
	return nil
}

func isAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	var apiErr *esclient.APIError
	if ok := errorAs(err, &apiErr); ok {
		return apiErr.StatusCode == http.StatusBadRequest
	}
	return false
}

// errorAs is a local wrapper because errors.As requires matching concrete types;
// the esclient wraps errors with fmt.Errorf which makes direct matching tricky.
// We also check the string as a fallback.
func errorAs(err error, target interface{}) bool {
	switch t := target.(type) {
	case **esclient.APIError:
		for e := err; e != nil; {
			if apiErr, ok := e.(*esclient.APIError); ok { //nolint:errorlint
				*t = apiErr
				return true
			}
			if u, ok := e.(interface{ Unwrap() error }); ok {
				e = u.Unwrap()
			} else {
				return false
			}
		}
	}
	return false
}

func countPressureIndices(esClient esclient.Client) (int, error) {
	req, err := http.NewRequest(http.MethodGet, "/_cat/indices/pressure-idx-*?format=json&h=index", nil) //nolint:noctx
	if err != nil {
		return 0, err
	}
	resp, err := esClient.Request(context.Background(), req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var indices []map[string]string
	if err := json.Unmarshal(body, &indices); err != nil {
		return 0, err
	}
	return len(indices), nil
}

func verifyShardCount(es esv1.Elasticsearch, k *test.K8sClient) error {
	esClient, err := elasticsearch.NewElasticsearchClient(es, k)
	if err != nil {
		return err
	}
	defer esClient.Close()

	health, err := esClient.GetClusterHealth(context.Background())
	if err != nil {
		return err
	}
	totalShards := health.ActiveShards + health.UnassignedShards + health.InitializingShards + health.RelocatingShards
	wantMin := numIndicesToCreate // at least 1 shard per index
	if totalShards < wantMin {
		return fmt.Errorf("total shards %d < minimum %d; waiting for shard allocation", totalShards, wantMin)
	}
	return nil
}

func buildGoClient() (kubernetes.Interface, error) {
	rules := clientcmd.NewDefaultClientConfigLoadingRules()
	overrides := &clientcmd.ConfigOverrides{}
	cfg, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(rules, overrides).ClientConfig()
	if err != nil {
		return nil, err
	}
	return kubernetes.NewForConfig(cfg)
}
