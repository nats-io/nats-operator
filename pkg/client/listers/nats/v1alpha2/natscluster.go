// Copyright 2017-2018 The nats-operator Authors
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

// Code generated by lister-gen. DO NOT EDIT.

package v1alpha2

import (
	v1alpha2 "github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// NatsClusterLister helps list NatsClusters.
// All objects returned here must be treated as read-only.
type NatsClusterLister interface {
	// List lists all NatsClusters in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha2.NatsCluster, err error)
	// NatsClusters returns an object that can list and get NatsClusters.
	NatsClusters(namespace string) NatsClusterNamespaceLister
	NatsClusterListerExpansion
}

// natsClusterLister implements the NatsClusterLister interface.
type natsClusterLister struct {
	indexer cache.Indexer
}

// NewNatsClusterLister returns a new NatsClusterLister.
func NewNatsClusterLister(indexer cache.Indexer) NatsClusterLister {
	return &natsClusterLister{indexer: indexer}
}

// List lists all NatsClusters in the indexer.
func (s *natsClusterLister) List(selector labels.Selector) (ret []*v1alpha2.NatsCluster, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha2.NatsCluster))
	})
	return ret, err
}

// NatsClusters returns an object that can list and get NatsClusters.
func (s *natsClusterLister) NatsClusters(namespace string) NatsClusterNamespaceLister {
	return natsClusterNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// NatsClusterNamespaceLister helps list and get NatsClusters.
// All objects returned here must be treated as read-only.
type NatsClusterNamespaceLister interface {
	// List lists all NatsClusters in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha2.NatsCluster, err error)
	// Get retrieves the NatsCluster from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha2.NatsCluster, error)
	NatsClusterNamespaceListerExpansion
}

// natsClusterNamespaceLister implements the NatsClusterNamespaceLister
// interface.
type natsClusterNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all NatsClusters in the indexer for a given namespace.
func (s natsClusterNamespaceLister) List(selector labels.Selector) (ret []*v1alpha2.NatsCluster, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha2.NatsCluster))
	})
	return ret, err
}

// Get retrieves the NatsCluster from the indexer for a given namespace and name.
func (s natsClusterNamespaceLister) Get(name string) (*v1alpha2.NatsCluster, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha2.Resource("natscluster"), name)
	}
	return obj.(*v1alpha2.NatsCluster), nil
}
