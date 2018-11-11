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

package client

import (
	"context"
	"errors"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"
)

type NatsClusterCR interface {
	// Create creates a NATS cluster CR with the desired CR
	Create(ctx context.Context, cl *v1alpha2.NatsCluster) (*v1alpha2.NatsCluster, error)

	// Get returns the specified NATS cluster CR
	Get(ctx context.Context, namespace, name string) (*v1alpha2.NatsCluster, error)

	// Delete deletes the specified NATS cluster CR
	Delete(ctx context.Context, namespace, name string) error

	// Update updates the NATS cluster CR.
	Update(ctx context.Context, natsCluster *v1alpha2.NatsCluster) (*v1alpha2.NatsCluster, error)
}

type natsClusterCR struct {
	client     *rest.RESTClient
	crScheme   *runtime.Scheme
	paramCodec runtime.ParameterCodec
}

func NewCRClient(cfg *rest.Config) (NatsClusterCR, error) {
	cli, crScheme, err := New(cfg)
	if err != nil {
		return nil, err
	}
	return &natsClusterCR{
		client:     cli,
		crScheme:   crScheme,
		paramCodec: runtime.NewParameterCodec(crScheme),
	}, nil
}

// TODO: make this private so that we don't expose RESTClient once operator code uses this client instead of REST calls
func New(cfg *rest.Config) (*rest.RESTClient, *runtime.Scheme, error) {
	crScheme := runtime.NewScheme()
	if err := v1alpha2.AddToScheme(crScheme); err != nil {
		return nil, nil, err
	}

	config := *cfg
	config.GroupVersion = &v1alpha2.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: serializer.NewCodecFactory(crScheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, nil, err
	}

	return client, crScheme, nil
}

func (c *natsClusterCR) Create(ctx context.Context, natsCluster *v1alpha2.NatsCluster) (*v1alpha2.NatsCluster, error) {
	if len(natsCluster.Namespace) == 0 {
		return nil, errors.New("need to set metadata.Namespace in NATS cluster CR")
	}
	result := &v1alpha2.NatsCluster{}
	err := c.client.Post().Context(ctx).
		Namespace(natsCluster.Namespace).
		Resource(v1alpha2.CRDResourcePlural).
		Body(natsCluster).
		Do().
		Into(result)
	return result, err
}

func (c *natsClusterCR) Get(ctx context.Context, namespace, name string) (*v1alpha2.NatsCluster, error) {
	result := &v1alpha2.NatsCluster{}
	err := c.client.Get().Context(ctx).
		Namespace(namespace).
		Resource(v1alpha2.CRDResourcePlural).
		Name(name).
		Do().
		Into(result)
	return result, err
}

func (c *natsClusterCR) Delete(ctx context.Context, namespace, name string) error {
	return c.client.Delete().Context(ctx).
		Namespace(namespace).
		Resource(v1alpha2.CRDResourcePlural).
		Name(name).
		Do().
		Error()
}

func (c *natsClusterCR) Update(ctx context.Context, natsCluster *v1alpha2.NatsCluster) (*v1alpha2.NatsCluster, error) {
	if len(natsCluster.Namespace) == 0 {
		return nil, errors.New("need to set metadata.Namespace in NATS cluster CR")
	}
	if len(natsCluster.Name) == 0 {
		return nil, errors.New("need to set metadata.Name in NATS cluster CR")
	}
	result := &v1alpha2.NatsCluster{}
	err := c.client.Put().Context(ctx).
		Namespace(natsCluster.Namespace).
		Resource(v1alpha2.CRDResourcePlural).
		Name(natsCluster.Name).
		Body(natsCluster).
		Do().
		Into(result)
	return result, err
}
