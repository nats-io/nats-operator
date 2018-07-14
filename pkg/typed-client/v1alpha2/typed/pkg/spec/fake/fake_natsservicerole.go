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

// FakeNatsServiceRoles implements NatsServiceRoleInterface
type FakeNatsServiceRoles struct {
	Fake *FakePkgSpec
	ns   string
}

var natsservicerolesResource = schema.GroupVersionResource{Group: "pkg", Version: "spec", Resource: "natsserviceroles"}

var natsservicerolesKind = schema.GroupVersionKind{Group: "pkg", Version: "spec", Kind: "NatsServiceRole"}

// Get takes name of the natsServiceRole, and returns the corresponding natsServiceRole object, and an error if there is any.
func (c *FakeNatsServiceRoles) Get(name string, options v1.GetOptions) (result *spec.NatsServiceRole, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewGetAction(natsservicerolesResource, c.ns, name), &spec.NatsServiceRole{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsServiceRole), err
}

// List takes label and field selectors, and returns the list of NatsServiceRoles that match those selectors.
func (c *FakeNatsServiceRoles) List(opts v1.ListOptions) (result *spec.NatsServiceRoleList, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewListAction(natsservicerolesResource, natsservicerolesKind, c.ns, opts), &spec.NatsServiceRoleList{})

	if obj == nil {
		return nil, err
	}

	label, _, _ := testing.ExtractFromListOptions(opts)
	if label == nil {
		label = labels.Everything()
	}
	list := &spec.NatsServiceRoleList{}
	for _, item := range obj.(*spec.NatsServiceRoleList).Items {
		if label.Matches(labels.Set(item.Labels)) {
			list.Items = append(list.Items, item)
		}
	}
	return list, err
}

// Watch returns a watch.Interface that watches the requested natsServiceRoles.
func (c *FakeNatsServiceRoles) Watch(opts v1.ListOptions) (watch.Interface, error) {
	return c.Fake.
		InvokesWatch(testing.NewWatchAction(natsservicerolesResource, c.ns, opts))

}

// Create takes the representation of a natsServiceRole and creates it.  Returns the server's representation of the natsServiceRole, and an error, if there is any.
func (c *FakeNatsServiceRoles) Create(natsServiceRole *spec.NatsServiceRole) (result *spec.NatsServiceRole, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewCreateAction(natsservicerolesResource, c.ns, natsServiceRole), &spec.NatsServiceRole{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsServiceRole), err
}

// Update takes the representation of a natsServiceRole and updates it. Returns the server's representation of the natsServiceRole, and an error, if there is any.
func (c *FakeNatsServiceRoles) Update(natsServiceRole *spec.NatsServiceRole) (result *spec.NatsServiceRole, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewUpdateAction(natsservicerolesResource, c.ns, natsServiceRole), &spec.NatsServiceRole{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsServiceRole), err
}

// Delete takes name of the natsServiceRole and deletes it. Returns an error if one occurs.
func (c *FakeNatsServiceRoles) Delete(name string, options *v1.DeleteOptions) error {
	_, err := c.Fake.
		Invokes(testing.NewDeleteAction(natsservicerolesResource, c.ns, name), &spec.NatsServiceRole{})

	return err
}

// DeleteCollection deletes a collection of objects.
func (c *FakeNatsServiceRoles) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	action := testing.NewDeleteCollectionAction(natsservicerolesResource, c.ns, listOptions)

	_, err := c.Fake.Invokes(action, &spec.NatsServiceRoleList{})
	return err
}

// Patch applies the patch and returns the patched natsServiceRole.
func (c *FakeNatsServiceRoles) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *spec.NatsServiceRole, err error) {
	obj, err := c.Fake.
		Invokes(testing.NewPatchSubresourceAction(natsservicerolesResource, c.ns, name, data, subresources...), &spec.NatsServiceRole{})

	if obj == nil {
		return nil, err
	}
	return obj.(*spec.NatsServiceRole), err
}
