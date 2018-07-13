package operatortests

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nats-io/nats-operator/pkg/client"
	"github.com/nats-io/nats-operator/pkg/controller"
	"github.com/nats-io/nats-operator/pkg/spec"
	natsalphav2client "github.com/nats-io/nats-operator/pkg/typed-client/v1alpha2/typed/pkg/spec"
	k8sv1 "k8s.io/api/core/v1"
	k8scrdclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	k8smetav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8swaitutil "k8s.io/apimachinery/pkg/util/wait"
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

func TestCreateConfigSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runController(ctx, t)

	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)

	name := "test-nats-cluster-1"
	namespace := "default"
	var size = 3
	cluster := &spec.NatsCluster{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       spec.CRDResourceKind,
			APIVersion: spec.SchemeGroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec.ClusterSpec{
			Size:    size,
			Version: "1.1.0",
		},
	}
	_, err = cl.ncli.Create(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the pods to be created
	params := k8smetav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=nats,nats_cluster=%s", name),
	}
	var podList *k8sv1.PodList
	err = k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		podList, err = cl.kc.Pods(namespace).List(params)
		if err != nil {
			return false, err
		}
		if len(podList.Items) < size {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be created: %s", err)
	}

	cm, err := cl.kc.Secrets(namespace).Get(name, k8smetav1.GetOptions{})
	if err != nil {
		t.Errorf("Config map error: %v", err)
	}
	conf, ok := cm.Data["nats.conf"]
	if !ok {
		t.Error("Config map was missing")
	}
	for _, pod := range podList.Items {
		if !strings.Contains(string(conf), pod.Name) {
			t.Errorf("Could not find pod %q in config", pod.Name)
		}
	}
}

func TestReplacePresentConfigSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	runController(ctx, t)

	cl, err := newKubeClients()
	if err != nil {
		t.Fatal(err)
	}
	// Wait for the CRD to be registered in the background.
	time.Sleep(10 * time.Second)
	name := "test-nats-cluster-2"
	namespace := "default"

	// Create a secret with the same name, that will
	// be replaced with a new one by the operator.
	cm := &k8sv1.Secret{
		ObjectMeta: k8smetav1.ObjectMeta{
			Name: name,
		},
		Data: map[string][]byte{
			"nats.conf": []byte("port: 4222"),
		},
	}
	_, err = cl.kc.Secrets(namespace).Create(cm)
	if err != nil {
		t.Fatal(err)
	}

	var size = 3
	cluster := &spec.NatsCluster{
		TypeMeta: k8smetav1.TypeMeta{
			Kind:       spec.CRDResourceKind,
			APIVersion: spec.SchemeGroupVersion.String(),
		},
		ObjectMeta: k8smetav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: spec.ClusterSpec{
			Size:    size,
			Version: "1.1.0",
		},
	}
	_, err = cl.ncli.Create(ctx, cluster)
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the pods to be created
	params := k8smetav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=nats,nats_cluster=%s", name),
	}
	var podList *k8sv1.PodList
	err = k8swaitutil.Poll(3*time.Second, 1*time.Minute, func() (bool, error) {
		podList, err = cl.kc.Pods(namespace).List(params)
		if err != nil {
			return false, err
		}
		if len(podList.Items) < size {
			return false, nil
		}

		return true, nil
	})
	if err != nil {
		t.Errorf("Error waiting for pods to be created: %s", err)
	}

	cm, err = cl.kc.Secrets(namespace).Get(name, k8smetav1.GetOptions{})
	if err != nil {
		t.Errorf("Config map error: %v", err)
	}
	conf, ok := cm.Data["nats.conf"]
	if !ok {
		t.Error("Config map was missing")
	}
	for _, pod := range podList.Items {
		if !strings.Contains(string(conf), pod.Name) {
			t.Errorf("Could not find pod %q in config", pod.Name)
		}
	}
}

type clients struct {
	kc      k8sclient.CoreV1Interface
	kcrdc   k8scrdclient.Interface
	restcli *k8srestapi.RESTClient
	config  *k8srestapi.Config
	ncli    client.NatsClusterCR
	ocli    *natsalphav2client.PkgSpecClient
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
	ncli, err := client.NewCRClient(cfg)
	if err != nil {
		return nil, err
	}
	ocli, err := natsalphav2client.NewForConfig(cfg)
	if err != nil {
		return nil, err
	}

	cl := &clients{
		kc:      kc,
		kcrdc:   kcrdc,
		restcli: restcli,
		ncli:    ncli,
		ocli:    ocli,
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
		Namespace:   "default",
		KubeCli:     cl.kc,
		KubeExtCli:  cl.kcrdc,
		OperatorCli: cl.ocli,
	}
	c := controller.New(config)

	// FIXME: These are set on the package
	controller.MasterHost = cl.config.Host
	controller.KubeHttpCli = cl.restcli.Client
	return c, nil
}

func runController(ctx context.Context, t *testing.T) {
	// Run the operator controller in the background.
	c, err := newController()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		err := c.Run(ctx)
		if err != nil && err != context.Canceled {
			t.Fatal(err)
		}
	}()
}
