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
	"time"

	"github.com/sirupsen/logrus"
	"k8s.io/api/core/v1"
	extsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	corev1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsclient "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	natsinformers "github.com/nats-io/nats-operator/pkg/client/informers/externalversions"
	natslisters "github.com/nats-io/nats-operator/pkg/client/listers/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/cluster"
	"github.com/nats-io/nats-operator/pkg/features"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
)

const (
	// kubeFullResyncPeriod is the period of time between every full resync of core/v1 resources by the shared informer factory.
	kubeFullResyncPeriod = 24 * time.Hour
	// natsClusterControllerDefaultThreadiness is the number of workers the NatsCluster controller will use to process items from the work queue.
	natsClusterControllerThreadiness = 2
	// natsFullResyncPeriod is the period of time between every full resync of nats.io/v1alpha2 resources by the shared informer factory.
	natsFullResyncPeriod = 24 * time.Hour
)

// Controller is the controller for NatsCluster resources.
type Controller struct {
	// NatsClusterController is based-off of a generic controller.
	*genericController
	// kubeInformerFactory allows us to create shared informers for the Kubernetes base API types.
	kubeInformerFactory kubeinformers.SharedInformerFactory
	// natsInformerFactory allows us to create shared informers for our API types.
	natsInformerFactory natsinformers.SharedInformerFactory
	// podLister is able to list/get Pod resources from a shared informer's store.
	podLister corev1listers.PodLister
	// secretLister is able to list/get Secret resources from a shared informer's store.
	secretLister corev1listers.SecretLister
	// serviceLister is able to list/get Service resources from a shared informer's store.
	serviceLister corev1listers.ServiceLister
	// natsClusterLister is able to list/get NatsCluster resources from a shared informer's store.
	natsClustersLister natslisters.NatsClusterLister
	// natsServiceRoleLister is able to list/get NatsServiceRole resources from a shared informer's store.
	natsServiceRoleLister natslisters.NatsServiceRoleLister

	logger *logrus.Entry

	Config
}

type Config struct {
	// FeatureMap is the map containing features and their status for the current instance of nats-operator.
	FeatureMap features.FeatureMap
	// NatsOperatorNamespace is the namespace under which the current instance of nats-operator is running.
	NatsOperatorNamespace string
	PVProvisioner         string
	KubeCli               kubernetes.Interface
	KubeConfig            *rest.Config
	KubeExtCli            extsclient.Interface
	OperatorCli           natsclient.Interface
}

func (c *Config) Validate() error {
	return nil
}

// informer is an interface that represents an informer such as a PodInformer.
type informer interface {
	Informer() cache.SharedIndexInformer
}

func NewNatsClusterController(cfg Config) *Controller {
	// Check if nats-operator is operating at cluster or namespace scope.
	// Based on this, we either watch all Kubernetes namespaces or just the one where nats-operator is deployed.
	var (
		watchedNamespace string
	)
	if cfg.FeatureMap.IsEnabled(features.ClusterScoped) {
		// Watch all Kubernetes namespaces.
		watchedNamespace = v1.NamespaceAll
	} else {
		// Watch only the Kubernetes namespace where nats-operator is deployed.
		watchedNamespace = cfg.NatsOperatorNamespace
	}
	// Create shared informer factories for the types we are interested in.
	// WithNamespace is used to filter resources belonging to "watchedNamespace".
	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(cfg.KubeCli, kubeFullResyncPeriod, kubeinformers.WithNamespace(watchedNamespace))
	natsInformerFactory := natsinformers.NewSharedInformerFactoryWithOptions(cfg.OperatorCli, natsFullResyncPeriod, natsinformers.WithNamespace(watchedNamespace))
	// Obtain references to shared informers for the required types.
	podInformer := kubeInformerFactory.Core().V1().Pods()
	secretInformer := kubeInformerFactory.Core().V1().Secrets()
	serviceInformer := kubeInformerFactory.Core().V1().Services()
	natsClustersInformer := natsInformerFactory.Nats().V1alpha2().NatsClusters()
	natsServiceRoleInformer := natsInformerFactory.Nats().V1alpha2().NatsServiceRoles()

	// Obtain references to listers for the required types.
	podLister := podInformer.Lister()
	secretLister := secretInformer.Lister()
	serviceLister := serviceInformer.Lister()
	natsClustersLister := natsClustersInformer.Lister()
	natsServiceRoleLister := natsServiceRoleInformer.Lister()

	// Create a new instance of Controller that uses the lister above.
	c := &Controller{
		genericController:     newGenericController(v1alpha2.CRDResourceKind, natsClusterControllerThreadiness),
		kubeInformerFactory:   kubeInformerFactory,
		natsInformerFactory:   natsInformerFactory,
		podLister:             podLister,
		secretLister:          secretLister,
		serviceLister:         serviceLister,
		natsClustersLister:    natsClustersLister,
		natsServiceRoleLister: natsServiceRoleLister,
		logger:                logrus.WithField("pkg", "controller"),
		Config:                cfg,
	}
	// Make the controller wait for caches to sync.
	c.hasSyncedFuncs = []cache.InformerSynced{
		podInformer.Informer().HasSynced,
		secretInformer.Informer().HasSynced,
		serviceInformer.Informer().HasSynced,
		natsClustersInformer.Informer().HasSynced,
		natsServiceRoleInformer.Informer().HasSynced,
	}
	// Make processQueueItem the handler for items popped out of the work queue.
	c.syncHandler = c.processQueueItem

	// Setup an event handler to inform us when NatsCluster resources change.
	natsClustersInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			clustersCreated.Inc()
			clustersTotal.Inc()
			c.enqueue(obj)
		},
		UpdateFunc: func(_, obj interface{}) {
			clustersModified.Inc()
			c.enqueue(obj)
		},
		DeleteFunc: func(obj interface{}) {
			clustersDeleted.Inc()
			clustersTotal.Dec()
			c.enqueue(obj)
		},
	})
	// Also setup event handlers to inform us when related resources (secrets, services, pods ans NatsClusterRoles) change.
	// This allows us to react promptly to, e.g., deleted pods or edited secrets.
	for _, inf := range []informer{podInformer, secretInformer, serviceInformer, natsServiceRoleInformer} {
		inf.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: c.handleObject,
			UpdateFunc: func(_, obj interface{}) {
				c.handleObject(obj)
			},
			DeleteFunc: c.handleObject,
		})
	}

	// Return the instance of Controller created above.
	return c
}

// processQueueItem attempts to reconcile the state of the NatsCluster resource pointed at by the specified key.
func (c *Controller) processQueueItem(key string) error {
	// Convert the namespace/name string into a distinct namespace and name.
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		runtime.HandleError(fmt.Errorf("invalid resource key: %s", key))
		return nil
	}

	// Get the NatsCluster resource with this namespace/name.
	natsCluster, err := c.natsClustersLister.NatsClusters(namespace).Get(name)
	if err != nil {
		// The NatsCluster resource may no longer exist.
		if kubernetesutil.IsKubernetesResourceNotFoundError(err) {
			c.logger.Warnf("natscluster %q was deleted", key)
			return nil
		}
		return err
	}

	// Often, new objects don't have their apiVersion and kind fields filled, so we fill them in manually to avoid errors.
	// https://github.com/kubernetes/apiextensions-apiserver/issues/29
	// We also create a deep copy of the original object in order to avoid mutating the cache.
	newObj := natsCluster.DeepCopy()
	newObj.TypeMeta.APIVersion = newObj.GetGroupVersionKind().GroupVersion().String()
	newObj.TypeMeta.Kind = newObj.GetGroupVersionKind().Kind
	newObj.Spec.Cleanup()
	return cluster.New(c.makeClusterConfig(), newObj).Reconcile()
}

func (c *Controller) Run(ctx context.Context) error {
	// Register our CRDs, waiting for them to become ready.
	err := kubernetesutil.InitCRDs(c.KubeExtCli)
	if err != nil {
		return err
	}

	// Start the shared informer factories.
	go c.kubeInformerFactory.Start(ctx.Done())
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

	// Block until the context is canceled.
	<-ctx.Done()
	return nil
}

// handleObject will take any resource implementing metav1.Object and attempt to find the NatsCluster resource that "owns" it.
// It does this by looking at the object's metadata.ownerReferences field for an appropriate OwnerReference.
// It then enqueues that NatsCluster resource to be processed.
// If the object does not have an appropriate OwnerReference, it may still be a NatsServiceRole that references the NatsCluster in its spec, so we check for that as well.
// Finally, the object may be a Secret referenced by one or more NatsCluster resources.
// In case the object doesn't match any of the conditions above, it is simply skipped.
func (c *Controller) handleObject(obj interface{}) {
	var (
		object metav1.Object
		ok     bool
	)

	if object, ok = obj.(metav1.Object); !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			runtime.HandleError(fmt.Errorf("failed to decode object: invalid type"))
			return
		}
		object, ok = tombstone.Obj.(metav1.Object)
		if !ok {
			runtime.HandleError(fmt.Errorf("failed to decode object tombstone: invalid type"))
			return
		}
		c.logger.Debugf("recovered deleted object %q from tombstone", kubernetesutil.ResourceKey(object))
	}

	c.logger.Debugf("processing object %q", kubernetesutil.ResourceKey(object))

	if ownerRef := metav1.GetControllerOf(object); ownerRef != nil {
		// If this object is not owned by a NatsCluster resource, we should not do anything more with it.
		if ownerRef.Kind != v1alpha2.CRDResourceKind {
			return
		}
		// Attempt to get the owning NatsCluster resource.
		natsCluster, err := c.natsClustersLister.NatsClusters(object.GetNamespace()).Get(ownerRef.Name)
		if err != nil {
			c.logger.Debugf("ignoring orphaned object %q of natscluster \"%s/%s\"", object.GetSelfLink(), object.GetNamespace(), ownerRef.Name)
			return
		}
		// Enqueue the NatsCluster resource for later processing.
		c.enqueue(natsCluster)
		return
	}

	// If the current resource is a NatsServiceRole, we must enqueue the NatsCluster referenced by ".metadata.labels.nats_cluster" so that its configuration is reconciled.
	if object, ok := obj.(*v1alpha2.NatsServiceRole); ok {
		c.enqueueByCoordinates(object.Namespace, object.Labels[kubernetesutil.LabelClusterNameKey])
		return
	}

	// If the current resource is a Secret, we must check whether there are any NatsCluster resources that references it via ".spec.auth.clientsAuthSecret" and enqueue them.
	if object, ok := obj.(*v1.Secret); ok {
		// List all NatsCluster resources in the same namespace as the current secret.
		clusters, err := c.natsClustersLister.NatsClusters(object.Namespace).List(labels.Everything())
		if err != nil {
			runtime.HandleError(fmt.Errorf("failed to list natscluster resources"))
			return
		}
		// Enqueue all NatsCluster resources which reference the current secret.
		for _, cluster := range clusters {
			if cluster.Spec.Auth != nil && cluster.Spec.Auth.ClientsAuthSecret == object.Name {
				c.enqueue(cluster)
			}
		}
		return
	}
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		KubeCli:               c.KubeCli.CoreV1(),
		OperatorCli:           c.OperatorCli.NatsV1alpha2(),
		PodLister:             c.podLister,
		SecretLister:          c.secretLister,
		ServiceLister:         c.serviceLister,
		NatsServiceRoleLister: c.natsServiceRoleLister,
		KubeClient:            c.KubeCli,
		KubeConfig:            c.KubeConfig,
		NatsClient:            c.OperatorCli,
	}
}
