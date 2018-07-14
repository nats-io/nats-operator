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
package spec

import (
	spec "github.com/nats-io/nats-operator/pkg/spec"
	scheme "github.com/nats-io/nats-operator/pkg/typed-client/v1alpha2/scheme"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	watch "k8s.io/apimachinery/pkg/watch"
	rest "k8s.io/client-go/rest"
)

// NatsServiceRolesGetter has a method to return a NatsServiceRoleInterface.
// A group's client should implement this interface.
type NatsServiceRolesGetter interface {
	NatsServiceRoles(namespace string) NatsServiceRoleInterface
}

// NatsServiceRoleInterface has methods to work with NatsServiceRole resources.
type NatsServiceRoleInterface interface {
	Create(*spec.NatsServiceRole) (*spec.NatsServiceRole, error)
	Update(*spec.NatsServiceRole) (*spec.NatsServiceRole, error)
	Delete(name string, options *v1.DeleteOptions) error
	DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error
	Get(name string, options v1.GetOptions) (*spec.NatsServiceRole, error)
	List(opts v1.ListOptions) (*spec.NatsServiceRoleList, error)
	Watch(opts v1.ListOptions) (watch.Interface, error)
	Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *spec.NatsServiceRole, err error)
	NatsServiceRoleExpansion
}

// natsServiceRoles implements NatsServiceRoleInterface
type natsServiceRoles struct {
	client rest.Interface
	ns     string
}

// newNatsServiceRoles returns a NatsServiceRoles
func newNatsServiceRoles(c *PkgSpecClient, namespace string) *natsServiceRoles {
	return &natsServiceRoles{
		client: c.RESTClient(),
		ns:     namespace,
	}
}

// Get takes name of the natsServiceRole, and returns the corresponding natsServiceRole object, and an error if there is any.
func (c *natsServiceRoles) Get(name string, options v1.GetOptions) (result *spec.NatsServiceRole, err error) {
	result = &spec.NatsServiceRole{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("natsserviceroles").
		Name(name).
		VersionedParams(&options, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// List takes label and field selectors, and returns the list of NatsServiceRoles that match those selectors.
func (c *natsServiceRoles) List(opts v1.ListOptions) (result *spec.NatsServiceRoleList, err error) {
	result = &spec.NatsServiceRoleList{}
	err = c.client.Get().
		Namespace(c.ns).
		Resource("natsserviceroles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do().
		Into(result)
	return
}

// Watch returns a watch.Interface that watches the requested natsServiceRoles.
func (c *natsServiceRoles) Watch(opts v1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.Get().
		Namespace(c.ns).
		Resource("natsserviceroles").
		VersionedParams(&opts, scheme.ParameterCodec).
		Watch()
}

// Create takes the representation of a natsServiceRole and creates it.  Returns the server's representation of the natsServiceRole, and an error, if there is any.
func (c *natsServiceRoles) Create(natsServiceRole *spec.NatsServiceRole) (result *spec.NatsServiceRole, err error) {
	result = &spec.NatsServiceRole{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("natsserviceroles").
		Body(natsServiceRole).
		Do().
		Into(result)
	return
}

// Update takes the representation of a natsServiceRole and updates it. Returns the server's representation of the natsServiceRole, and an error, if there is any.
func (c *natsServiceRoles) Update(natsServiceRole *spec.NatsServiceRole) (result *spec.NatsServiceRole, err error) {
	result = &spec.NatsServiceRole{}
	err = c.client.Put().
		Namespace(c.ns).
		Resource("natsserviceroles").
		Name(natsServiceRole.Name).
		Body(natsServiceRole).
		Do().
		Into(result)
	return
}

// Delete takes name of the natsServiceRole and deletes it. Returns an error if one occurs.
func (c *natsServiceRoles) Delete(name string, options *v1.DeleteOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("natsserviceroles").
		Name(name).
		Body(options).
		Do().
		Error()
}

// DeleteCollection deletes a collection of objects.
func (c *natsServiceRoles) DeleteCollection(options *v1.DeleteOptions, listOptions v1.ListOptions) error {
	return c.client.Delete().
		Namespace(c.ns).
		Resource("natsserviceroles").
		VersionedParams(&listOptions, scheme.ParameterCodec).
		Body(options).
		Do().
		Error()
}

// Patch applies the patch and returns the patched natsServiceRole.
func (c *natsServiceRoles) Patch(name string, pt types.PatchType, data []byte, subresources ...string) (result *spec.NatsServiceRole, err error) {
	result = &spec.NatsServiceRole{}
	err = c.client.Patch(pt).
		Namespace(c.ns).
		Resource("natsserviceroles").
		SubResource(subresources...).
		Name(name).
		Body(data).
		Do().
		Into(result)
	return
}
