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
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/watch"
	podutil "k8s.io/kubernetes/pkg/api/v1/pod"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/constants"
)

// natsPodContainer returns a NATS server pod container spec.
func natsPodContainer(container v1.Container, clusterName, version string, serverImage string, enableClientsHostPort bool, gatewayPort int, leafnodePort int) v1.Container {
	container.Name = constants.NatsContainerName
	container.Image = MakeNATSImage(version, serverImage)

	ports := []v1.ContainerPort{
		{
			Name:          "cluster",
			ContainerPort: int32(constants.ClusterPort),
			Protocol:      v1.ProtocolTCP,
		},
		{
			Name:          "monitoring",
			ContainerPort: int32(constants.MonitoringPort),
			Protocol:      v1.ProtocolTCP,
		},
	}

	port := v1.ContainerPort{
		Name:          "client",
		ContainerPort: int32(constants.ClientPort),
		Protocol:      v1.ProtocolTCP,
	}
	if enableClientsHostPort {
		port.HostPort = int32(constants.ClientPort)
	}
	ports = append(ports, port)

	if gatewayPort > 0 {
		port := v1.ContainerPort{
			Name:          "gateway",
			ContainerPort: int32(gatewayPort),
			Protocol:      v1.ProtocolTCP,
			HostPort:      int32(gatewayPort),
		}
		ports = append(ports, port)
	}
	if leafnodePort > 0 {
		port := v1.ContainerPort{
			Name:          "leaf",
			ContainerPort: int32(leafnodePort),
			Protocol:      v1.ProtocolTCP,
			HostPort:      int32(leafnodePort),
		}
		ports = append(ports, port)
	}
	container.Ports = ports

	return container
}

// natsPodReloaderContainer returns a NATS server pod container spec for configuration reloader.
func natsPodReloaderContainer(image, tag, pullPolicy, authFilePath string, r v1.ResourceRequirements) v1.Container {
	container := v1.Container{
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
		Resources: r,
	}
	if authFilePath != "" {
		// The volume is mounted as a subdirectory under the NATS config.
		af := filepath.Join(constants.ConfigMapMountPath, authFilePath)
		container.Command = append(container.Command, "-config", af)
	}
	return container
}

// natsPodMetricsContainer returns a NATS server pod container spec for prometheus metrics exporter.
func natsPodMetricsContainer(image, tag, pullPolicy string) v1.Container {
	return v1.Container{
		Name:            "metrics",
		Image:           fmt.Sprintf("%s:%s", image, tag),
		ImagePullPolicy: v1.PullPolicy(pullPolicy),
		Command:         []string{},
		Ports: []v1.ContainerPort{
			{
				Name:          "metrics",
				ContainerPort: int32(constants.MetricsPort),
				Protocol:      v1.ProtocolTCP,
			},
		},
		Args: []string{
			"-connz",
			"-routez",
			"-subz",
			"-varz",
			"-channelz",
			"-serverz",
			fmt.Sprintf("http://localhost:%d", constants.MonitoringPort)},
	}
}

func containerWithLivenessProbe(c v1.Container, lp *v1.Probe) v1.Container {
	c.LivenessProbe = lp
	return c
}

func containerWithRequirements(c v1.Container, r v1.ResourceRequirements) v1.Container {
	c.Resources = r
	return c
}

func natsLivenessProbe(cs v1alpha2.ClusterSpec) *v1.Probe {
	action := &v1.HTTPGetAction{
		Port: intstr.IntOrString{IntVal: constants.MonitoringPort},
	}
	if cs.TLS != nil && cs.TLS.EnableHttps {
		action.Scheme = "HTTPS"
	}

	return &v1.Probe{
		Handler: v1.Handler{
			HTTPGet: action,
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

func applyPodPolicy(clusterName string, pod *v1.Pod, policy *v1alpha2.PodPolicy) {
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

	mergeMaps(pod.Labels, policy.Labels)
	mergeMaps(pod.Annotations, policy.Annotations)

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

// WaitUntilPodCondition establishes a watch on the specified pod and blocks until the specified condition function is satisfied.
func WaitUntilPodCondition(ctx context.Context, kubeClient corev1.CoreV1Interface, pod *v1.Pod, fn watch.ConditionFunc) error {
	// Create a selector that targets the specified pod.
	fs := ByCoordinates(pod.Namespace, pod.Name)
	// Grab a ListerWatcher with which we can watch the pod.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fs.String()
			return kubeClient.Pods(pod.Namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watchapi.Interface, error) {
			options.FieldSelector = fs.String()
			return kubeClient.Pods(pod.Namespace).Watch(options)
		},
	}
	// Watch for updates to the specified pod until fn is satisfied.
	last, err := watch.UntilWithSync(ctx, lw, &v1.Pod{}, nil, fn)
	if err != nil {
		return err
	}
	if last == nil {
		return fmt.Errorf("no events received for pod %q", ResourceKey(pod))
	}
	return nil
}

// isPodRunningAndReady returns whether the specified pod is running, ready and has its ".status.podIP" field populated.
func isPodRunningAndReady(pod *v1.Pod) bool {
	return pod.Status.Phase == v1.PodRunning && podutil.IsPodReady(pod) && pod.Status.PodIP != ""
}

// WaitUntilPodReady establishes a watch on the specified pod and blocks until the pod is running, ready and has its ".status.podIP" field populated.
func WaitUntilPodReady(ctx context.Context, kubeClient corev1.CoreV1Interface, pod *v1.Pod) error {
	return WaitUntilPodCondition(ctx, kubeClient, pod, func(event watchapi.Event) (bool, error) {
		switch event.Type {
		case watchapi.Error:
			return false, fmt.Errorf("got event of type error: %+v", event.Object)
		case watchapi.Deleted:
			return false, fmt.Errorf("pod %q has been deleted", ResourceKey(pod))
		default:
			pod = event.Object.(*v1.Pod)
			return isPodRunningAndReady(pod), nil
		}
	})
}

// WaitUntilDeploymentCondition establishes a watch on the specified deployment and blocks until the specified condition function is satisfied.
func WaitUntilDeploymentCondition(ctx context.Context, kubeClient kubernetes.Interface, namespace, name string, fn watch.ConditionFunc) error {
	// Create a selector that targets the specified deployment.
	fs := ByCoordinates(namespace, name)
	// Grab a ListerWatcher with which we can watch the deployment.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = fs.String()
			return kubeClient.AppsV1().Deployments(namespace).List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watchapi.Interface, error) {
			options.FieldSelector = fs.String()
			return kubeClient.AppsV1().Deployments(namespace).Watch(options)
		},
	}
	// Watch for updates to the specified deployment until fn is satisfied.
	last, err := watch.UntilWithSync(ctx, lw, &appsv1.Deployment{}, nil, fn)
	if err != nil {
		return err
	}
	if last == nil {
		return fmt.Errorf("no events received for deployment \"%s/%s\"", namespace, name)
	}
	return nil
}
