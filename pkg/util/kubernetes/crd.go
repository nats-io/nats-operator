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
	"encoding/json"
	"errors"
	"fmt"
	"time"

	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	"github.com/nats-io/nats-operator/pkg/debug/local"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"
)

var (
	ErrCRDAlreadyExists = errors.New("crd already exists")
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

func GetClusterCRDObject(restcli rest.Interface, ns, name string) (*v1alpha2.NatsCluster, error) {
	uri := fmt.Sprintf("/apis/%s/namespaces/%s/%s/%s", v1alpha2.SchemeGroupVersion.String(), ns, v1alpha2.CRDResourcePlural, name)
	b, err := restcli.Get().RequestURI(uri).DoRaw()
	if err != nil {
		return nil, err
	}
	return readClusterCR(b)
}

func UpdateClusterCRDObject(restcli rest.Interface, ns string, c *v1alpha2.NatsCluster) (*v1alpha2.NatsCluster, error) {
	uri := fmt.Sprintf("/apis/%s/namespaces/%s/%s/%s", v1alpha2.SchemeGroupVersion.String(), ns, v1alpha2.CRDResourcePlural, c.Name)
	b, err := restcli.Put().RequestURI(uri).Body(c).DoRaw()
	if err != nil {
		return nil, err
	}
	return readClusterCR(b)
}

func readClusterCR(b []byte) (*v1alpha2.NatsCluster, error) {
	cluster := &v1alpha2.NatsCluster{}
	if err := json.Unmarshal(b, cluster); err != nil {
		return nil, fmt.Errorf("read cluster CR from json data failed: %v", err)
	}
	return cluster, nil
}

func CreateCRD(clientset apiextensionsclient.Interface) error {
	// Lookup in case the CRDs are both present already.
	_, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(v1alpha2.CRDName, metav1.GetOptions{})
	_, err2 := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(v1alpha2.ServiceRoleCRDName, metav1.GetOptions{})
	if err == nil && err2 == nil {
		return ErrCRDAlreadyExists
	}

	// NatsCluster
	crd := &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: v1alpha2.CRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   v1alpha2.SchemeGroupVersion.Group,
			Version: v1alpha2.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural:     v1alpha2.CRDResourcePlural,
				Kind:       v1alpha2.CRDResourceKind,
				ShortNames: []string{"nats"},
			},
		},
	}

	_, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err != nil && !IsKubernetesResourceAlreadyExistError(err) {
		return err
	}

	// NatsServiceRole
	crd = &apiextensionsv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: v1alpha2.ServiceRoleCRDName,
		},
		Spec: apiextensionsv1beta1.CustomResourceDefinitionSpec{
			Group:   v1alpha2.SchemeGroupVersion.Group,
			Version: v1alpha2.SchemeGroupVersion.Version,
			Scope:   apiextensionsv1beta1.NamespaceScoped,
			Names: apiextensionsv1beta1.CustomResourceDefinitionNames{
				Plural: v1alpha2.ServiceRoleCRDResourcePlural,
				Kind:   v1alpha2.ServiceRoleCRDResourceKind,
			},
		},
	}
	_, err2 = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(crd)
	if err2 != nil && !IsKubernetesResourceAlreadyExistError(err2) {
		return err2
	}

	return nil
}

func WaitCRDReady(clientset apiextensionsclient.Interface) error {
	err := retryutil.Retry(5*time.Second, 20, func() (bool, error) {
		crd, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(v1alpha2.CRDName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range crd.Status.Conditions {
			switch cond.Type {
			case apiextensionsv1beta1.Established:
				if cond.Status == apiextensionsv1beta1.ConditionTrue {
					return true, nil
				}
			case apiextensionsv1beta1.NamesAccepted:
				if cond.Status == apiextensionsv1beta1.ConditionFalse {
					return false, fmt.Errorf("Name conflict: %v", cond.Reason)
				}
			}
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("wait CRD created failed: %v", err)
	}
	return nil
}

func MustNewKubeExtClient() apiextensionsclient.Interface {
	var (
		cfg *rest.Config
		err error
	)

	if len(local.KubeConfigPath) == 0 {
		cfg, err = InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", local.KubeConfigPath)
	}

	if err != nil {
		panic(err)
	}

	return apiextensionsclient.NewForConfigOrDie(cfg)
}
