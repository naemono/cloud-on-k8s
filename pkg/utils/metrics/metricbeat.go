package metrics

import (
	"fmt"

	"golang.org/x/net/context"
	v1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/elastic/cloud-on-k8s/v2/pkg/controller/common/operator"
	"github.com/elastic/cloud-on-k8s/v2/pkg/utils/pointer"
)

func StartMetricBeat(clientset kubernetes.Interface, metricsPort int, parameters operator.Parameters) error {
	beatYml := fmt.Sprintf(`http:
  enabled: false
metricbeat.modules:
  # Metrics collected from a Prometheus endpoint
  - module: prometheus
	period: 10s
	metricsets: ["collector"]
	hosts: ["elastic-operator-0:%d"]
	metrics_path: /metrics
`, metricsPort)
	configmapClient := clientset.CoreV1().ConfigMaps(parameters.OperatorNamespace)
	_, err := configmapClient.Get(context.Background(), "metricsbeat-config", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = configmapClient.Create(context.Background(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "metricsbeat-config",
					Namespace: parameters.OperatorNamespace,
				},
				Data: map[string]string{
					"metricbeat.yml": beatYml,
				},
			}, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("while creating metricsbeat configmap: %w", err)
			}
		} else {
			return fmt.Errorf("while getting metricsbeat-config configmap: %w", err)
		}
	}
	c := clientset.AppsV1().Deployments(parameters.OperatorNamespace)
	_, err = c.Get(context.Background(), "metricsbeat", metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			_, err = c.Create(context.Background(), &v1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "metricsbeat",
					Namespace: parameters.OperatorNamespace,
				},
				Spec: v1.DeploymentSpec{
					Replicas: pointer.Int32(1),
					Template: corev1.PodTemplateSpec{
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
			}, metav1.CreateOptions{})
			if err != nil {
				return fmt.Errorf("while creating metricsbeat deployment: %w", err)
			}
		} else {
			return fmt.Errorf("while getting metricsbeat deployment: %w", err)
		}
	}
	return nil
}
