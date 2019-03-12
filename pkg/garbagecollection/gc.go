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

package garbagecollection

import (
	"sync"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	appsv1beta1 "k8s.io/client-go/kubernetes/typed/apps/v1beta1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

const (
	NullUID = ""
)

var pkgLogger = logrus.WithField("pkg", "gc")

type GC struct {
	logger             *logrus.Entry
	kubecli            corev1client.CoreV1Interface
	kubecliAppsv1beta1 appsv1beta1.AppsV1beta1Interface
}

func New(kubecli corev1client.CoreV1Interface) *GC {
	return &GC{
		logger:             pkgLogger,
		kubecli:            kubecli,
		kubecliAppsv1beta1: appsv1beta1.New(kubecli.RESTClient()),
	}
}

// CollectCluster collects resources that matches cluster lable, but
// does not belong to the cluster with given clusterUID
func (gc *GC) CollectCluster(namespace, cluster string, clusterUID types.UID) {
	gc.collectResources(namespace, kubernetesutil.ClusterListOpt(cluster), map[types.UID]bool{clusterUID: true})
}

// FullyCollect collects resources that were created before,
// but does not belong to any current running clusters.
func (gc *GC) FullyCollect(namespace string) {
	// Compute the list of namespaces on which to perform garbage collection.
	var (
		namespaces []string
	)
	switch namespace {
	case v1.NamespaceAll:
		logrus.Debugf("performing garbage collection across all namespaces")
		// Grab the list of namespaces for the current Kubernetes cluster.
		n, err := gc.kubecli.Namespaces().List(metav1.ListOptions{})
		if err != nil {
			logrus.Errorf("failed to list namespaces: %v", err)
			return
		}
		// Add each namespace to the list of namespaces to consider.
		for _, namespace := range n.Items {
			namespaces = append(namespaces, namespace.Name)
		}
	default:
		logrus.Debugf("performing garbage collection in namespace %q", namespace)
		// Add the specified namespace to the list of namespaces to consider.
		namespaces = append(namespaces, namespace)
	}

	// Perform garbage collection in parallel in each of the considered namespaces.
	var (
		wg sync.WaitGroup
	)
	for _, namespace := range namespaces {
		wg.Add(1)
		go func(namespace string) {
			defer wg.Done()
			clusters, err := kubernetesutil.GetClusterList(gc.kubecli.RESTClient(), namespace)
			if err != nil {
				logrus.Errorf("failed to list nats clusters in namespace %q: %v", namespace, err)
				return
			}
			clusterUIDSet := make(map[types.UID]bool)
			for _, c := range clusters.Items {
				clusterUIDSet[c.UID] = true
			}
			option := metav1.ListOptions{
				LabelSelector: labels.SelectorFromSet(map[string]string{
					"app": "nats",
				}).String(),
			}
			gc.collectResources(namespace, option, clusterUIDSet)
		}(namespace)
	}
	wg.Wait()
}

func (gc *GC) collectResources(namespace string, option metav1.ListOptions, runningSet map[types.UID]bool) {
	if err := gc.collectPods(namespace, option, runningSet); err != nil {
		gc.logger.Errorf("gc pods failed: %v", err)
	}
	if err := gc.collectServices(namespace, option, runningSet); err != nil {
		gc.logger.Errorf("gc services failed: %v", err)
	}
}

func (gc *GC) collectPods(namespace string, option metav1.ListOptions, runningSet map[types.UID]bool) error {
	pods, err := gc.kubecli.Pods(namespace).List(option)
	if err != nil {
		return err
	}

	for _, p := range pods.Items {
		if len(p.OwnerReferences) == 0 {
			gc.logger.Warningf("failed to check pod %s: no owner", p.GetName())
			continue
		}
		// Pods failed due to liveness probe are also collected
		if !runningSet[p.OwnerReferences[0].UID] || p.Status.Phase == v1.PodFailed {
			// kill bad pods without grace period to kill it immediately
			err = gc.kubecli.Pods(namespace).Delete(p.GetName(), metav1.NewDeleteOptions(0))
			if err != nil && !kubernetesutil.IsKubernetesResourceNotFoundError(err) {
				return err
			}
			gc.logger.Infof("deleted pod \"%s/%s\"", p.Namespace, p.Name)
		}
	}
	return nil
}

func (gc *GC) collectConfigMaps(namespace string, option metav1.ListOptions, runningSet map[types.UID]bool) error {
	cms, err := gc.kubecli.Secrets(namespace).List(option)
	if err != nil {
		return err
	}

	for _, cm := range cms.Items {
		if len(cm.OwnerReferences) == 0 {
			gc.logger.Warningf("failed to check service %s: no owner", cm.GetName())
			continue
		}
		if !runningSet[cm.OwnerReferences[0].UID] {
			err = gc.kubecli.Secrets(namespace).Delete(cm.GetName(), nil)
			if err != nil && !kubernetesutil.IsKubernetesResourceNotFoundError(err) {
				return err
			}
			gc.logger.Infof("deleted configmap \"%s/%s\"", cm.Namespace, cm.Name)
		}
	}

	return nil
}

func (gc *GC) collectServices(namespace string, option metav1.ListOptions, runningSet map[types.UID]bool) error {
	srvs, err := gc.kubecli.Services(namespace).List(option)
	if err != nil {
		return err
	}

	for _, srv := range srvs.Items {
		if len(srv.OwnerReferences) == 0 {
			gc.logger.Warningf("failed to check service %s: no owner", srv.GetName())
			continue
		}
		if !runningSet[srv.OwnerReferences[0].UID] {
			err = gc.kubecli.Services(namespace).Delete(srv.GetName(), nil)
			if err != nil && !kubernetesutil.IsKubernetesResourceNotFoundError(err) {
				return err
			}
			gc.logger.Infof("deleted service \"%s/%s\"", srv.Namespace, srv.Name)
		}
	}

	return nil
}
