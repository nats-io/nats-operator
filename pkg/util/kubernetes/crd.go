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
	"reflect"
	"time"

	"github.com/sirupsen/logrus"
	extsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	extsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	watchapi "k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/watch"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
)

const (
	// waitCRDReadyTimeout is the maximum period of time we wait for each CRD to be ready (i.e. established).
	waitCRDReadyTimeout = 30 * time.Second
)

var (
	// crds contains all the custom resource definitions that nats-operator registers upon starting.
	crds = []*extsv1beta1.CustomResourceDefinition{
		// NatsCluster
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: v1alpha2.CRDName,
			},
			Spec: extsv1beta1.CustomResourceDefinitionSpec{
				Group:   v1alpha2.SchemeGroupVersion.Group,
				Version: v1alpha2.SchemeGroupVersion.Version,
				Scope:   extsv1beta1.NamespaceScoped,
				Names: extsv1beta1.CustomResourceDefinitionNames{
					Plural:     v1alpha2.CRDResourcePlural,
					Kind:       v1alpha2.CRDResourceKind,
					ShortNames: []string{"nats"},
				},
			},
		},
		// NatsServiceRole
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: v1alpha2.ServiceRoleCRDName,
			},
			Spec: extsv1beta1.CustomResourceDefinitionSpec{
				Group:   v1alpha2.SchemeGroupVersion.Group,
				Version: v1alpha2.SchemeGroupVersion.Version,
				Scope:   extsv1beta1.NamespaceScoped,
				Names: extsv1beta1.CustomResourceDefinitionNames{
					Plural: v1alpha2.ServiceRoleCRDResourcePlural,
					Kind:   v1alpha2.ServiceRoleCRDResourceKind,
				},
			},
		},
	}
)

// TODO: replace this package with Operator client

// NatsClusterCRUpdateFunc is a function to be used when atomically
// updating a Cluster CR.
type NatsClusterCRUpdateFunc func(*v1alpha2.NatsCluster)

func GetClusterList(restcli rest.Interface, ns string) (*v1alpha2.NatsClusterList, error) {
	b, err := restcli.Get().RequestURI(listClustersURI(ns)).DoRaw()
	if err != nil {
		return nil, err
	}

	clusters := &v1alpha2.NatsClusterList{}
	if err := json.Unmarshal(b, clusters); err != nil {
		return nil, err
	}
	return clusters, nil
}

func listClustersURI(ns string) string {
	return fmt.Sprintf("/apis/%s/namespaces/%s/%s", v1alpha2.SchemeGroupVersion.String(), ns, v1alpha2.CRDResourcePlural)
}

// MustNewKubeExtClient creates a new client for the apiextensions.k8s.io/v1beta1 API.
func MustNewKubeExtClient(cfg *rest.Config) extsclientset.Interface {
	return extsclientset.NewForConfigOrDie(cfg)
}

// InitCRDs registers the CRDs for the nats.io/v1alpha2 API and waits for them to become ready.
func InitCRDs(extsClient extsclientset.Interface) error {
	for _, crd := range crds {
		// Create the CustomResourceDefinition in the api.
		if err := createOrUpdateCRD(crd, extsClient); err != nil {
			return err
		}
	}
	return WaitCRDs(extsClient)
}

// WaitCRDs waits for the CRDs to become ready.
func WaitCRDs(extsClient extsclientset.Interface) error {
	for _, crd := range crds {
		// Wait for the CustomResourceDefinition to be established.
		if err := waitCRDReady(crd, extsClient); err != nil {
			return err
		}
	}
	return nil
}

// createOrUpdateCRD creates or updates the specified custom resource definition according to the provided specification.
func createOrUpdateCRD(crd *extsv1beta1.CustomResourceDefinition, extsClient extsclientset.Interface) error {
	// At this point the CRD may already exist from manual creation. Attempt to get the CRD
	d, err := extsClient.ApiextensionsV1beta1().CustomResourceDefinitions().Get(crd.Name, metav1.GetOptions{})

	// CRD already exists, but it's what we expect.
	if err == nil && reflect.DeepEqual(d.Spec, crd.Spec) {
		return nil
	}

	// CRD already exists, and is different than what is expected.
	if err == nil {
		// Attempt to update the CRD by setting its spec to the expected value.
		d.Spec = crd.Spec
		_, err = extsClient.ApiextensionsV1beta1().CustomResourceDefinitions().Update(d)
		return err
	}

	// No CRD existed, attempt to register the CRD.
	_, err = extsClient.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	return err
}

// waitCRDReady blocks until the specified custom resource definition has been established and is ready for being used.
func waitCRDReady(crd *extsv1beta1.CustomResourceDefinition, extsClient extsclientset.Interface) error {
	// Grab a ListerWatcher with which we can watch the CRD.
	lw := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = ByCoordinates(crd.Namespace, crd.Name).String()
			return extsClient.ApiextensionsV1beta1().CustomResourceDefinitions().List(options)
		},
		WatchFunc: func(options metav1.ListOptions) (watchapi.Interface, error) {
			options.FieldSelector = ByCoordinates(crd.Namespace, crd.Name).String()
			return extsClient.ApiextensionsV1beta1().CustomResourceDefinitions().Watch(options)
		},
	}

	// Watch for updates to the specified CRD until it reaches the "Established" state or until "waitCRDReadyTimeout" elapses.
	ctx, fn := context.WithTimeout(context.Background(), waitCRDReadyTimeout)
	defer fn()
	last, err := watch.UntilWithSync(ctx, lw, &extsv1beta1.CustomResourceDefinition{}, nil, func(event watchapi.Event) (bool, error) {
		// Grab the current resource from the event.
		obj := event.Object.(*extsv1beta1.CustomResourceDefinition)
		// Return true if and only if the CRD is ready.
		return isReady(obj), nil
	})
	if err != nil {
		// We've got an error while watching the specified CRD.
		return err
	}
	if last == nil {
		// We've got no events for the CRD, which most probably means registration is stuck.
		return fmt.Errorf("no events received for crd %q", crd.Name)
	}

	// At this point we are sure the CRD is ready, so we return.
	logrus.Debugf("crd %q established", crd.Spec.Names.Kind)
	return nil
}

// isReady returns whether the specified CRD is ready to be used, by searching for "Established" in its conditions.
func isReady(crd *extsv1beta1.CustomResourceDefinition) bool {
	for _, cond := range crd.Status.Conditions {
		switch cond.Type {
		case extsv1beta1.Established:
			if cond.Status == extsv1beta1.ConditionTrue {
				return true
			}
		}
	}
	return false
}
