// Copyright 2016 The nats-operator Authors
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

package k8sutil

import (
	"encoding/json"

	"github.com/pires/nats-operator/pkg/constants"

	"k8s.io/kubernetes/pkg/api"
	unversionedAPI "k8s.io/kubernetes/pkg/api/unversioned"
	"k8s.io/kubernetes/pkg/util/intstr"
)

// natsPodContainer returns a NATS server pod container spec.
func natsPodContainer(args []string, version string) api.Container {
	c := api.Container{
		Name:            "nats",
		Image:           MakeNATSImage(version),
		ImagePullPolicy: api.PullAlways,
		Args:            args,
		Ports: []api.ContainerPort{
			{
				Name:          "cluster",
				ContainerPort: int32(constants.ClusterPort),
				Protocol:      api.ProtocolTCP,
			},
			{
				Name:          "client",
				ContainerPort: int32(constants.ClientPort),
				Protocol:      api.ProtocolTCP,
			},
			{
				Name:          "monitoring",
				ContainerPort: int32(constants.MonitoringPort),
				Protocol:      api.ProtocolTCP,
			},
		},
		// a NATS pod is alive when monitoring API is up.
		LivenessProbe: &api.Probe{
			Handler: api.Handler{
				HTTPGet: &api.HTTPGetAction{
					Port: intstr.IntOrString{IntVal: constants.MonitoringPort},
				},
			},
			InitialDelaySeconds: 10,
			TimeoutSeconds:      10,
			// probe every 60 seconds
			PeriodSeconds: 60,
			// failed for 3 minutes
			FailureThreshold: 3,
		},
		// TODO use for TLS
		//VolumeMounts: []api.VolumeMount{
		//	{Name: "nats-data", MountPath: rootDir},
		//},
	}

	return c
}

// PodWithAntiAffinity sets pod anti-affinity with the pods in the same NATS cluster
func PodWithAntiAffinity(pod *api.Pod, clusterName string) *api.Pod {
	affinity := api.Affinity{
		PodAntiAffinity: &api.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []api.PodAffinityTerm{
				{
					LabelSelector: &unversionedAPI.LabelSelector{
						MatchLabels: map[string]string{
							"nats_cluster": clusterName,
						},
					},
					TopologyKey: "kubernetes.io/hostname",
				},
			},
		},
	}

	affinityb, err := json.Marshal(affinity)
	if err != nil {
		panic("failed to marshal affinty struct")
	}

	pod.Annotations[api.AffinityAnnotationKey] = string(affinityb)
	return pod
}
