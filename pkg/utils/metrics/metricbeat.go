package metrics

import (
	"fmt"

	"golang.org/x/net/context"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/pointer"
)

func StartMetricBeat(clnt client.Client, metricsPort int, namespace, esURL, password string) error {
	beatYml := fmt.Sprintf(`http:
  enabled: false
metricbeat.modules:
  # Metrics collected from a Prometheus endpoint
  - module: prometheus
	period: 10s
	metricsets: ["collector"]
	hosts: ["elastic-operator-0:%d"]
	metrics_path: /metrics
output:
	elasticsearch:
	  hosts:
	  - %s
	  password: %s
	  ssl:
		verification_mode: none
	  username: elastic
`, metricsPort, esURL, password)
	var cm corev1.ConfigMap
	err := clnt.Get(context.Background(), types.NamespacedName{Name: "metricsbeat-config", Namespace: namespace}, &cm)
	if err != nil {
		if errors.IsNotFound(err) {
			err = clnt.Create(context.Background(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "metricsbeat-config",
					Namespace: namespace,
				},
				Data: map[string]string{
					"metricbeat.yml": beatYml,
				},
			})
			if err != nil {
				return fmt.Errorf("while creating metricsbeat configmap: %w", err)
			}
		} else {
			return fmt.Errorf("while getting metricsbeat-config configmap: %w", err)
		}
	}
	var deployment v1.Deployment
	err = clnt.Get(context.Background(), types.NamespacedName{Name: "metricsbeat", Namespace: namespace}, &deployment)
	if err != nil {
		if errors.IsNotFound(err) {
			err = clnt.Create(context.Background(), &v1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "metricsbeat",
					Namespace: namespace,
				},
				Spec: v1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "eck-operator-metricsbeat",
						},
					},
					Replicas: pointer.Int32(1),
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "eck-operator-metricsbeat",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "metricbeat-config",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: pointer.Int32(0600),
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "metricsbeat-config",
											},
										},
									},
								},
							},
							Containers: []corev1.Container{
								{
									Name:            "metricbeat",
									Image:           "docker.elastic.co/beats/metricbeat:8.5.0",
									ImagePullPolicy: corev1.PullIfNotPresent,
									Args:            []string{"-e"},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "metricbeat-config",
											MountPath: "/usr/shared/metricbeat/metricbeat.yml",
											ReadOnly:  true,
											SubPath:   "metricbeat.yml",
										},
									},
								},
							},
						},
					},
				},
			})
			if err != nil {
				return fmt.Errorf("while creating metricsbeat deployment: %w", err)
			}
		} else {
			return fmt.Errorf("while getting metricsbeat deployment: %w", err)
		}
	}
	return nil
}
