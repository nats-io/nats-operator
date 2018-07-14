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
package fake

import (
	spec "github.com/nats-io/nats-operator/pkg/spec"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	labels "k8s.io/apimachinery/pkg/labels"
	schema "k8s.io/apimachinery/pkg/runtime/schema"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	testing "k8s.io/client-go/testing"
)

// FakeNatsClusters implements NatsClusterInterface
type FakeNatsClusters struct {
	Fake *FakePkgSpec
	ns   string
}

var natsclustersResource = schema.GroupVersionResource{Group: "pkg", Version: "spec", Resource: "natsclusters"}

var natsclustersKind = schema.GroupVersionKind{Group: "pkg", Version: "spec", Kind: "NatsCluster"}

// Get takes name of the natsCluster, and returns the corresponding natsCluster object, and an error if there is any.
func (c *FakeNatsClusters) Get(name string, options v1.GetOptions) (result *spec.NatsCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(natsclustersResource, c.ns, name), &spec.NatsCluster{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsCluster), err
}

// List takes label and field selectors, and returns the list of NatsClusters that match those selectors.
func (c *FakeNatsClusters) List(opts v1.ListOptions) (result *spec.NatsClusterList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(natsclustersResource, natsclustersKind, c.ns, opts), &spec.NatsClusterList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &spec.NatsClusterList{}
	for _, item := range obj.(*spec.NatsClusterList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested natsClusters.
func (c *FakeNatsClusters) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(natsclustersResource, c.ns, opts))

}

// Create takes the representation of a natsCluster and creates it.  Returns the server's representation of the natsCluster, and an error, if there is any.
func (c *FakeNatsClusters) Create(natsCluster *spec.NatsCluster) (result *spec.NatsCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(natsclustersResource, c.ns, natsCluster), &spec.NatsCluster{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsCluster), err
}

// Update takes the representation of a natsCluster and updates it. Returns the server's representation of the natsCluster, and an error, if there is any.
func (c *FakeNatsClusters) Update(natsCluster *spec.NatsCluster) (result *spec.NatsCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(natsclustersResource, c.ns, natsCluster), &spec.NatsCluster{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsCluster), err
}

// Delete takes name of the natsCluster and deletes it. Returns an error if one occurs.
func (c *FakeNatsClusters) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(natsclustersResource, c.ns, name), &spec.NatsCluster{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNatsClusters) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(natsclustersResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &spec.NatsClusterList{})
	return err
}

// Patch applies the patch and returns the patched natsCluster.
func (c *FakeNatsClusters) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *spec.NatsCluster, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(natsclustersResource, c.ns, name, data, subresources...), &spec.NatsCluster{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsCluster), err
}
