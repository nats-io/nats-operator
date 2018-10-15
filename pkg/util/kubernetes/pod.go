// Copyright 2017 The nats-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kubernetes

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/spec"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// natsPodContainer returns a NATS server pod container spec.
func natsPodContainer(clusterName string, version string) v1.Container {
	return v1.Container{
		Env: []v1.EnvVar{
			{
				Name:  "SVC",
				Value: ManagementServiceName(clusterName),
			},
			{
				Name:  "EXTRA",
				Value: fmt.Sprintf("--http_port=%d", constants.MonitoringPort),
			},
		},
		Name:  "nats",
		Image: MakeNATSImage(version),
		Ports: []v1.ContainerPort{
			{
				Name:          "cluster",
				ContainerPort: int32(constants.ClusterPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "client",
				ContainerPort: int32(constants.ClientPort),
				Protocol:      v1.ProtocolTCP,
			},
			{
				Name:          "monitoring",
				ContainerPort: int32(constants.MonitoringPort),
				Protocol:      v1.ProtocolTCP,
			},
		},
	}
}

// reloaderContainer returns a NATS server pod container spec.
func natsPodReloaderContainer(image, tag, pullPolicy string) v1.Container {
	return v1.Container{
		Name:            "reloader",
		Image:           fmt.Sprintf("%s:%s", image, tag),
		ImagePullPolicy: v1.PullPolicy(pullPolicy),
		Command: []string{
			"nats-server-config-reloader",
			"-config",
			constants.ConfigFilePath,
			"-pid",
			constants.PidFilePath,
		},
	}
}

// natsExporterPodContainer returns a NATS exporter pod container spec.
func natsExporterPodContainer(clusterName string) v1.Container {
	c := v1.Container{
		Name:  "prometheus-exporter",
		Image: NATSPrometheusExporterImage,
		Ports: []v1.ContainerPort{
			{
				Name:          "prometheus",
				ContainerPort: int32(constants.NatsPrometheusExporterPort),
				Protocol:      v1.ProtocolTCP,
			},
		},
	}

	return c
}

func containerWithLivenessProbe(c v1.Container, lp *v1.Probe) v1.Container {
	c.LivenessProbe = lp
	return c
}

func containerWithRequirements(c v1.Container, r v1.ResourceRequirements) v1.Container {
	c.Resources = r
	return c
}

func natsLivenessProbe() *v1.Probe {
	return &v1.Probe{
		Handler: v1.Handler{
			HTTPGet: &v1.HTTPGetAction{
				Port: intstr.IntOrString{IntVal: constants.MonitoringPort},
			},
		},
		InitialDelaySeconds: 10,
		TimeoutSeconds:      10,
		PeriodSeconds:       60,
		FailureThreshold:    3,
	}
}

// PodWithAntiAffinity sets pod anti-affinity with the pods in the same NATS cluster
func PodWithAntiAffinity(pod *v1.Pod, clusterName string) *v1.Pod {
	ls := &metav1.LabelSelector{MatchLabels: map[string]string{
		LabelClusterNameKey: clusterName,
	}}
	return podWithAntiAffinity(pod, ls)
}

func podWithAntiAffinity(pod *v1.Pod, ls *metav1.LabelSelector) *v1.Pod {
	affinity := &v1.Affinity{
		PodAntiAffinity: &v1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []v1.PodAffinityTerm{
				{
					LabelSelector: ls,
					TopologyKey:   "kubernetes.io/hostname",
				},
			},
		},
	}

	pod.Spec.Affinity = affinity
	return pod
}

func applyPodPolicy(clusterName string, pod *v1.Pod, policy *spec.PodPolicy) {
	if policy == nil {
		return
	}

	if policy.AntiAffinity {
		pod = PodWithAntiAffinity(pod, clusterName)
	}

	if len(policy.NodeSelector) != 0 {
		pod = PodWithNodeSelector(pod, policy.NodeSelector)
	}
	if len(policy.Tolerations) != 0 {
		pod.Spec.Tolerations = policy.Tolerations
	}

	mergeLabels(pod.Labels, policy.Labels)

	for i := range pod.Spec.Containers {
		if pod.Spec.Containers[i].Name == "nats" {
			pod.Spec.Containers[i].Env = append(pod.Spec.Containers[i].Env, policy.NatsEnv...)
		}
	}
}

// IsPodReady returns false if the Pod Status is nil
func IsPodReady(pod *v1.Pod) bool {
	condition := getPodReadyCondition(&pod.Status)
	return condition != nil && condition.Status == v1.ConditionTrue
}

func getPodReadyCondition(status *v1.PodStatus) *v1.PodCondition {
	for i := range status.Conditions {
		if status.Conditions[i].Type == v1.PodReady {
			return &status.Conditions[i]
		}
	}
	return nil
}

func PodSpecToPrettyJSON(pod *v1.Pod) (string, error) {
	bytes, err := json.MarshalIndent(pod.Spec, "", "    ")
	if err != nil {
		return "", err
	}
	return string(bytes), nil
}
