package operatortests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/pires/nats-operator/pkg/client"
	"github.com/pires/nats-operator/pkg/controller"
	k8scrdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sclient "k8s.io/client-go/kubernetes/typed/core/v1"
	k8srestapi "k8s.io/client-go/rest"
	k8sclientcmd "k8s.io/client-go/tools/clientcmd"
)

func TestRegisterCRD(t *testing.T) {
	c, err := newController()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Run the operator controller in the background.
	go func() {
		err := c.Run(ctx)
		if err != nil && err != context.Canceled {
			t.Fatal(err)
		}
	}()

	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)

	// Confirm that the resource has been created.
	result, err := cl.kcrdc.ApiextensionsV1beta1().
		CustomResourceDefinitions().
		Get("natsclusters.nats.io", k8smetav1.GetOptions{})
	if err != nil {
		t.Errorf("Failed registering cluster: %s", err)
	}

	got := result.Spec.Names
	expected := "natsclusters"
	if got.Plural != expected {
		t.Errorf("got: %s, expected: %s", got.Plural, expected)
	}
	expected = "natscluster"
	if got.Singular != expected {
		t.Errorf("got: %s, expected: %s", got.Plural, expected)
	}
	if len(got.ShortNames) < 1 {
		t.Errorf("expected shortnames for the CRD: %+v", got.ShortNames)
	}
	expected = "nats"
	if got.ShortNames[0] != expected {
		t.Errorf("got: %s, expected: %s", got.ShortNames[0], expected)
	}
}

type clients struct {
	kc      k8sclient.CoreV1Interface
	kcrdc   k8scrdclient.Interface
	restcli *k8srestapi.RESTClient
	config  *k8srestapi.Config
}

func newKubeClients() (*clients, error) {
	var err error
	var cfg *k8srestapi.Config
	if kubeconfig := os.Getenv("KUBERNETES_CONFIG_FILE"); kubeconfig != "" {
		cfg, err = k8sclientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	kc, err := k8sclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	kcrdc, err := k8scrdclient.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}
	restcli, _, err := client.New(cfg)
	if err != nil {
		return nil, err
	}

	cl := &clients{
		kc:      kc,
		kcrdc:   kcrdc,
		restcli: restcli,
		config:  cfg,
	}
	return cl, nil
}

func newController() (*controller.Controller, error) {
	cl, err := newKubeClients()
	if err != nil {
		return nil, err
	}

	// NOTE: Eventually use a namespace at random under a
	// delete propagation policy for deleting the namespace.
	config := controller.Config{
		Namespace:  "default",
		KubeCli:    cl.kc,
		KubeExtCli: cl.kcrdc,
	}
	c := controller.New(config)

	// FIXME: These are set on the package
	controller.MasterHost = cl.config.Host
	controller.KubeHttpCli = cl.restcli.Client
	return c, nil
}
