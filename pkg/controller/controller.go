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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/nats-io/nats-operator/pkg/cluster"
	"github.com/nats-io/nats-operator/pkg/spec"
	natsalphav2client "github.com/nats-io/nats-operator/pkg/typed-client/v1alpha2/typed/pkg/spec"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/probe"

	"github.com/sirupsen/logrus"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	kwatch "k8s.io/apimachinery/pkg/watch"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	ErrVersionOutdated = errors.New("requested version is outdated in apiserver")

	initRetryWaitTime = 30 * time.Second

	// Workaround for watching CR resource.
	// TODO: remove this to use CR client.
	KubeHttpCli *http.Client
	MasterHost  string
)

type Event struct {
	Type   kwatch.EventType
	Object *spec.NatsCluster
}

type Controller struct {
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
	KubeExtCli     apiextensionsclient.Interface
	OperatorCli    natsalphav2client.PkgSpecInterface
}

func (c *Config) Validate() error {
	return nil
}

func New(cfg Config) *Controller {
	return &Controller{
		logger: logrus.WithField("pkg", "controller"),

		Config:     cfg,
		clusters:   make(map[string]*cluster.Cluster),
		clusterRVs: make(map[string]string),
		stopChMap:  map[string]chan struct{}{},
	}
}

func (c *Controller) Run(ctx context.Context) error {
	var (
		watchVersion string
		err          error
	)

	for {
		watchVersion, err = c.initResource()
		if err == nil {
			break
		}
		c.logger.Errorf("initialization failed: %v", err)
		c.logger.Infof("retry in %v...", initRetryWaitTime)
		time.Sleep(initRetryWaitTime)
		// todo: add max retry?
	}

	c.logger.Infof("starts running from watch version: %s", watchVersion)

	defer func() {
		for _, stopC := range c.stopChMap {
			close(stopC)
		}
		c.waitCluster.Wait()
	}()

	probe.SetReady()

	eventCh, errCh := c.watch(watchVersion)

	go func() {
		pt := newPanicTimer(time.Minute, "unexpected long blocking (> 1 Minute) when handling cluster event")

		for ev := range eventCh {
			pt.start()
			if err := c.handleClusterEvent(ev); err != nil {
				c.logger.Warningf("fail to handle event: %v", err)
			}
			pt.stop()
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

func (c *Controller) handleClusterEvent(event *Event) error {
	clus := event.Object

	if clus.Status.IsFailed() {
		clustersFailed.Inc()
		if event.Type == kwatch.Deleted {
			delete(c.clusters, clus.Name)
			delete(c.clusterRVs, clus.Name)
			return nil
		}
		return fmt.Errorf("ignore failed cluster (%s). Please delete its CR", clus.Name)
	}

	// TODO: add validation to spec update.
	clus.Spec.Cleanup()

	switch event.Type {
	case kwatch.Added:
		if _, ok := c.clusters[clus.Name]; ok {
			return fmt.Errorf("unsafe state. cluster (%s) was created before but we received event (%s)", clus.Name, event.Type)
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
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		c.clusters[clus.Name].Update(clus)
		c.clusterRVs[clus.Name] = clus.ResourceVersion
		clustersModified.Inc()

	case kwatch.Deleted:
		if _, ok := c.clusters[clus.Name]; !ok {
			return fmt.Errorf("unsafe state. cluster (%s) was never created but we received event (%s)", clus.Name, event.Type)
		}
		c.clusters[clus.Name].Delete()
		delete(c.clusters, clus.Name)
		delete(c.clusterRVs, clus.Name)
		clustersDeleted.Inc()
		clustersTotal.Dec()
	}
	return nil
}

func (c *Controller) findAllClusters() (string, error) {
	c.logger.Info("finding existing clusters...")
	clusterList, err := kubernetesutil.GetClusterList(c.Config.KubeCli.RESTClient(), c.Config.Namespace)
	if err != nil {
		return "", err
	}

	for i := range clusterList.Items {
		clus := clusterList.Items[i]

		if clus.Status.IsFailed() {
			c.logger.Infof("ignore failed cluster (%s). Please delete its CR", clus.Name)
			continue
		}

		clus.Spec.Cleanup()

		stopC := make(chan struct{})
		nc := cluster.New(c.makeClusterConfig(), &clus, stopC, &c.waitCluster)
		c.stopChMap[clus.Name] = stopC
		c.clusters[clus.Name] = nc
		c.clusterRVs[clus.Name] = clus.ResourceVersion
	}

	return clusterList.ResourceVersion, nil
}

func (c *Controller) makeClusterConfig() cluster.Config {
	return cluster.Config{
		ServiceAccount: c.Config.ServiceAccount,
		KubeCli:        c.KubeCli,
		OperatorCli:    c.OperatorCli,
	}
}

func (c *Controller) initResource() (string, error) {
	watchVersion := "0"
	err := c.initCRD()
	if err == kubernetesutil.ErrCRDAlreadyExists {
		return c.findAllClusters()
	}
	if err != nil {
		return "", fmt.Errorf("fail to create CRD: %v", err)
	}

	return watchVersion, nil
}

func (c *Controller) initCRD() error {
	err := kubernetesutil.CreateCRD(c.KubeExtCli)
	if err != nil {
		return err
	}
	return kubernetesutil.WaitCRDReady(c.KubeExtCli)
}

// watch creates a go routine, and watches the cluster.nats kind resources from
// the given watch version. It emits events on the resources through the returned
// event chan. Errors will be reported through the returned error chan. The go routine
// exits on any error.
func (c *Controller) watch(watchVersion string) (<-chan *Event, <-chan error) {
	eventCh := make(chan *Event)
	// On unexpected error case, controller should exit
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)

		for {
			resp, err := kubernetesutil.WatchClusters(MasterHost, c.Config.Namespace, KubeHttpCli, watchVersion)
			if err != nil {
				errCh <- err
				return
			}
			if resp.StatusCode != http.StatusOK {
				resp.Body.Close()
				errCh <- errors.New("invalid status code: " + resp.Status)
				return
			}

			c.logger.Infof("start watching at %v", watchVersion)

			decoder := json.NewDecoder(resp.Body)
			for {
				ev, st, err := pollEvent(decoder)
				if err != nil {
					// API Server will close stream periodically so schedule a reconnect,
					// also recover in case connection was broken for some reason.
					if err == io.EOF || err == io.ErrUnexpectedEOF {
						c.logger.Info("apiserver closed watch stream, retrying after 5s...")
						time.Sleep(5 * time.Second)
						break
					}

					c.logger.Errorf("received invalid event from API server: %v", err)
					errCh <- err
					return
				}

				if st != nil {
					resp.Body.Close()

					if st.Code == http.StatusGone {
						// event history is outdated.
						// if nothing has changed, we can go back to watch again.
						clusterList, err := kubernetesutil.GetClusterList(c.Config.KubeCli.RESTClient(), c.Config.Namespace)
						if err == nil && !c.isClustersCacheStale(clusterList.Items) {
							watchVersion = clusterList.ResourceVersion
							break
						}

						// if anything has changed (or error on relist), we have to rebuild the state.
						// go to recovery path
						errCh <- ErrVersionOutdated
						return
					}

					c.logger.Fatalf("unexpected status response from API server: %v", st.Message)
				}

				c.logger.Debugf("NATS cluster event: %v %v", ev.Type, ev.Object.Spec)

				watchVersion = ev.Object.ResourceVersion
				eventCh <- ev
			}

			resp.Body.Close()
		}
	}()

	return eventCh, errCh
}

func (c *Controller) isClustersCacheStale(currentClusters []spec.NatsCluster) bool {
	if len(c.clusterRVs) != len(currentClusters) {
		return true
	}

	for _, cc := range currentClusters {
		rv, ok := c.clusterRVs[cc.Name]
		if !ok || rv != cc.ResourceVersion {
			return true
		}
	}

	return false
}
