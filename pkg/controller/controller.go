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

package controller

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	extsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kwatch "k8s.io/apimachinery/pkg/watch"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsclient "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	natsinformers "github.com/nats-io/nats-operator/pkg/client/informers/externalversions"
	natslisters "github.com/nats-io/nats-operator/pkg/client/listers/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/cluster"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/probe"
)

const (
	// natsClusterControllerDefaultThreadiness is the number of workers the NatsCluster controller will use to process items from the work queue.
	natsClusterControllerThreadiness = 2
)

// Controller is the controller for NatsCluster resources.
type Controller struct {
	// NatsClusterController is based-off of a generic controller.
	*genericController
	// natsClusterLister is able to list/get NatsCluster resources from a shared informer's store.
	natsClustersLister natslisters.NatsClusterLister
	// natsInformerFactory allows us to create shared informers for our API types.
	natsInformerFactory natsinformers.SharedInformerFactory

	logger *logrus.Entry
	Config

	// TODO: combine the three cluster map.
	clusters map[string]*cluster.Cluster
	// Kubernetes resource version of the clusters
	clusterRVs map[string]string
	stopChMap  map[string]chan struct{}

	waitCluster sync.WaitGroup
}

type Config struct {
	Namespace      string
	ServiceAccount string
	PVProvisioner  string
	KubeCli        corev1client.CoreV1Interface
	KubeExtCli     extsclient.Interface
	OperatorCli    natsclient.Interface
}

func (c *Config) Validate() error {
	return nil
}

// NewNatsClusterController returns a new instance of a controller for NatsCluster resources.
func NewNatsClusterController(cfg Config) *Controller {
	// Create a shared informer factory for our API.
	natsInformerFactory := natsinformers.NewSharedInformerFactory(cfg.OperatorCli, time.Second*30)
	// Obtain references to shared informers for the required types.
	natsClustersInformer := natsInformerFactory.Nats().V1alpha2().NatsClusters()
	// Obtain references to listers for the required types.
	natsClustersLister := natsClustersInformer.Lister()

	// Create a new instance of Controller that uses the lister above.
	c := &Controller{
		genericController:   newGenericController(v1alpha2.CRDResourceKind, natsClusterControllerThreadiness),
		natsInformerFactory: natsInformerFactory,
		natsClustersLister:  natsClustersLister,

		logger: logrus.WithField("pkg", "controller"),

		Config:     cfg,
		clusters:   make(map[string]*cluster.Cluster),
		clusterRVs: make(map[string]string),
		stopChMap:  map[string]chan struct{}{},
	}
	// Make the controller wait for caches to sync.
	c.hasSyncedFuncs = []cache.InformerSynced{
		natsClustersInformer.Informer().HasSynced,
	}

	// Setup an event handler to inform us when NatsCluster resources change.
	natsClustersInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			c.handleEvent(kwatch.Added, obj.(*v1alpha2.NatsCluster))
		},
		UpdateFunc: func(newObj, oldObj interface{}) {
			c.handleEvent(kwatch.Modified, oldObj.(*v1alpha2.NatsCluster))
		},
		DeleteFunc: func(obj interface{}) {
			c.handleEvent(kwatch.Deleted, obj.(*v1alpha2.NatsCluster))
		},
	})

	// Return the instance of Controller created above.
	return c
}

// handleEvent is a temporary helper function that pipes events from the shared index informer to the existing handleClusterEvent method.
// It is used while support for using the controller's work queue isn't implemented.
func (c *Controller) handleEvent(eventType kwatch.EventType, obj *v1alpha2.NatsCluster) {
	// Ignore objects in different namespaces while we don't implement multi-namespace support.
	if obj.Namespace != c.Namespace {
		c.logger.Warnf("ignoring %q event for %q in namespace %q", eventType, obj.Name, obj.Namespace)
		return
	}
	// Often, new objects don't have their apiVersion and kind fields filled, so we fill them in manually to avoid errors.
	// https://github.com/kubernetes/apiextensions-apiserver/issues/29
	// We also create a deep copy of the original object in order to avoid mutating the cache.
	newObj := obj.DeepCopy()
	newObj.TypeMeta.APIVersion = obj.GetGroupVersionKind().GroupVersion().String()
	newObj.TypeMeta.Kind = obj.GetGroupVersionKind().Kind
	c.handleClusterEvent(eventType, newObj)
}

func (c *Controller) Run(ctx context.Context) error {
	// Register our CRDs, waiting for them to become ready.
	err := c.initCRD()
	if err != nil && err != kubernetesutil.ErrCRDAlreadyExists {
		return err
	}

	// Start the shared informer factory.
	go c.natsInformerFactory.Start(ctx.Done())

	// Handle any possible crashes and shutdown the workqueue when we're done.
	defer runtime.HandleCrash()
	defer c.workqueue.ShutDown()

	c.logger.Debug("starting controller")

	// Wait for the caches to be synced before starting workers.
	c.logger.Debug("waiting for informer caches to sync")
	if ok := cache.WaitForCacheSync(ctx.Done(), c.hasSyncedFuncs...); !ok {
		return fmt.Errorf("failed to wait for informer caches to sync")
	}

	c.logger.Debug("starting workers")

	// Launch "threadiness" workers to process work items.
	for i := 0; i < c.threadiness; i++ {
		go wait.Until(c.runWorker, time.Second, ctx.Done())
	}

	c.logger.Info("started workers")

	// Signal that we're ready and wait for the context to be canceled.
	probe.SetReady()

	defer func() {
		for _, stopC := range c.stopChMap {
			close(stopC)
		}
		c.waitCluster.Wait()
	}()

	// Block until the context is canceled.
	<-ctx.Done()
	return nil
}

func (c *Controller) handleClusterEvent(eventType kwatch.EventType, obj *v1alpha2.NatsCluster) error {
	clus := obj

	if clus.Status.IsFailed() {
		clustersFailed.Inc()
		if eventType == kwatch.Deleted {
			delete(c.clusters, clus.Name)
			delete(c.clusterRVs, clus.Name)
			return nil
		}
		return fmt.Errorf("ignore failed cluster (%s). Please delete its CR", clus.Name)
	}

	// TODO: add validation to spec update.
	clus.Spec.Cleanup()

	switch eventType {
	case kwatch.Added:
		if _, ok := c.clusters[clus.Name]; ok {
			return fmt.Errorf("unsafe state. cluster (%s) was created before but we received event (%s)", clus.Name, eventType)
		}

		stopC := make(chan struct{})
		nc := cluster.New(c.makeClusterConfig(), clus, stopC, &c.waitCluster)

		c.stopChMap[clus.Name] = stopC
		c.clusters[clus.Name] = nc
		c.clusterRVs[clus.Name] = clus.ResourceVersion

		clustersCreated.Inc()
		clustersTotal.Inc()

	case kwatch.Modified:
		if _, ok := c.clusters[clus.Name]; !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, eventType)
		}
		c.clusters[clus.Name].Update(clus)
		c.clusterRVs[clus.Name] = clus.ResourceVersion
		clustersModified.Inc()

	case kwatch.Deleted:
		if _, ok := c.clusters[clus.Name]; !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, eventType)
		}
		c.clusters[clus.Name].Delete()
		delete(c.clusters, clus.Name)
		delete(c.clusterRVs, clus.Name)
		clustersDeleted.Inc()
		clustersTotal.Dec()
	}
	return nil
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ServiceAccount: c.Config.ServiceAccount,
		KubeCli:        c.KubeCli,
		OperatorCli:    c.OperatorCli.NatsV1alpha2(),
	}
}

// initCRD registers the CRDs for our API and waits for them to become ready.
func (c *Controller) initCRD() error {
	err := kubernetesutil.CreateCRD(c.KubeExtCli)
	if err != nil {
		return err
	}
	return kubernetesutil.WaitCRDReady(c.KubeExtCli)
}
