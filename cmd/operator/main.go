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

package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"
	"k8s.io/api/core/v1"
	extsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"

	"github.com/nats-io/nats-operator/pkg/chaos"
	natsclientset "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/controller"
	"github.com/nats-io/nats-operator/pkg/debug"
	"github.com/nats-io/nats-operator/pkg/debug/local"
	"github.com/nats-io/nats-operator/pkg/garbagecollection"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/probe"
	"github.com/nats-io/nats-operator/version"
)

var (
	namespace  string
	name       string
	listenAddr string
	gcInterval time.Duration

	chaosLevel int

	printVersion bool

	// clusterScoped indicates whether the current instance of nats-operator is operating cluster-wide.
	clusterScoped bool
)

func init() {
	flag.BoolVar(&clusterScoped, "experimental-cluster-scoped", false, "[EXPERIMENTAL] whether nats-operator should manage resources across all namespaces")
	flag.StringVar(&debug.DebugFilePath, "debug-logfile-path", "", "only for a self hosted cluster, the path where the debug logfile will be written, recommended to be under: /var/tmp/nats-operator/debug/ to avoid any issue with lack of write permissions")
	flag.StringVar(&local.KubeConfigPath, "debug-kube-config-path", "", "the path to the local 'kubectl' config file (only for local debugging)")
	flag.StringVar(&local.PodName, "debug-pod-name", "nats-operator-debug", "the name of the pod which to report to EventRecorder (only for local debugging).")

	flag.StringVar(&listenAddr, "listen-addr", "0.0.0.0:8080", "The address on which the HTTP server will listen to")
	// chaos level will be removed once we have a formal tool to inject failures.
	flag.IntVar(&chaosLevel, "chaos-level", -1, "DO NOT USE IN PRODUCTION - level of chaos injected into the nats clusters created by the operator.")
	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
	flag.DurationVar(&gcInterval, "gc-interval", 10*time.Minute, "GC interval")
	flag.Parse()
}

func main() {
	formatter := &logrus.TextFormatter{
		FullTimestamp: true,
	}
	logrus.SetFormatter(formatter)

	namespace = os.Getenv("MY_POD_NAMESPACE")
	if len(namespace) == 0 {
		logrus.Fatalf("must set env MY_POD_NAMESPACE")
	}
	// Force cluster-scoped instances of nats-operator to run on the "nats-io" namespace.
	// This is the simplest way to guarantee that leader election occurs as expected because all cluster-scoped instances will do resource locking on this same namespace.
	if clusterScoped && namespace != constants.KubernetesNamespaceNatsIO {
		logrus.Fatalf("cluster-scoped instances of nats-operator must run on the %q namespace", constants.KubernetesNamespaceNatsIO)
	}

	if len(local.KubeConfigPath) == 0 {
		name = os.Getenv("MY_POD_NAME")
	} else {
		name = local.PodName
	}
	if len(name) == 0 {
		logrus.Fatalf("must set env MY_POD_NAME (or --debug-pod-name for local debugging)")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		logrus.Infof("received signal: %v", <-c)
		os.Exit(1)
	}()

	if printVersion {
		fmt.Println("nats-operator Version:", version.OperatorVersion)
		fmt.Println("Git SHA:", version.GitSHA)
		fmt.Println("Go Version:", runtime.Version())
		fmt.Printf("Go OS/Arch: %s/%s\n", runtime.GOOS, runtime.GOARCH)
		os.Exit(0)
	}

	logrus.Infof("nats-operator Version: %v", version.OperatorVersion)
	logrus.Infof("Git SHA: %s", version.GitSHA)
	logrus.Infof("Go Version: %s", runtime.Version())
	logrus.Infof("Go OS/Arch: %s/%s", runtime.GOOS, runtime.GOARCH)

	if clusterScoped {
		logrus.Warnf("nats-operator is operating at the cluster scope (experimental)")
	}

	id, err := os.Hostname()
	if err != nil {
		logrus.Fatalf("failed to get hostname: %v", err)
	}

	// Parse the specified kubeconfig and grab both a configuration object and a Kubernetes client.
	// The configuration object is required to create clients for our API later on.
	kubeCfg := kubernetesutil.MustNewKubeConfig(local.KubeConfigPath)
	kubeClient := kubernetesutil.MustNewKubeClientFromConfig(kubeCfg)

	http.HandleFunc(probe.HTTPReadyzEndpoint, probe.ReadyzHandler)
	go http.ListenAndServe(listenAddr, nil)

	rl, err := resourcelock.New(resourcelock.EndpointsResourceLock,
		namespace,
		"nats-operator",
		kubeClient.CoreV1(),
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: createRecorder(kubeClient.CoreV1(), name, namespace),
		})
	if err != nil {
		logrus.Fatalf("error creating lock: %v", err)
	}

	// Signal that we're ready.
	// We do it right before leader election so that we can use a "rolling update" strategy to update "nats-operator" while keeping unavailability to the bare minimum.
	probe.SetReady()

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: func(ctx context.Context) {
				run(ctx, kubeCfg, kubeClient)
			},
			OnStoppedLeading: func() {
				logrus.Fatalf("leader election lost")
			},
		},
	})

	panic("unreachable")
}

func run(ctx context.Context, kubeCfg *rest.Config, kubeClient kubernetes.Interface) {
	// Create a client for the apiextensions.k8s.io/v1beta1 so that we can register our CRDs.
	extsClient := kubernetesutil.MustNewKubeExtClient(kubeCfg)
	// Create a client for our API so that we can create shared index informers for our API types.
	natsClient := kubernetesutil.MustNewNatsClientFromConfig(kubeCfg)

	// Create a new controller configuration object.
	cfg := newControllerConfig(kubeCfg, kubeClient, extsClient, natsClient)
	if err := cfg.Validate(); err != nil {
		logrus.Fatalf("invalid operator config: %v", err)
	}
	// Initialize the controller for NatsCluster resources.
	c := controller.NewNatsClusterController(cfg)

	// Start the garbage collector.
	var (
		gcNamespace string
	)
	if clusterScoped {
		gcNamespace = v1.NamespaceAll
	} else {
		gcNamespace = namespace
	}
	go periodicFullGC(cfg.KubeCli.CoreV1(), gcNamespace, gcInterval)

	// Start the chaos engine if the current instance is not cluster-scoped.
	if !clusterScoped {
		startChaos(context.Background(), cfg.KubeCli.CoreV1(), cfg.NatsOperatorNamespace, chaosLevel)
	}

	// Run the controller for NatsCluster resources.
	if err := c.Run(ctx); err != nil {
		logrus.Fatalf("unexpected error while running the main control loop: %v", err)
	}
}

func newControllerConfig(kubeConfig *rest.Config, kubeClient kubernetes.Interface, extsClient extsclientset.Interface, natsClient natsclientset.Interface) controller.Config {
	return controller.Config{
		ClusterScoped:         clusterScoped,
		NatsOperatorNamespace: namespace,
		KubeCli:               kubeClient,
		KubeExtCli:            extsClient,
		OperatorCli:           natsClient,
		KubeConfig:            kubeConfig,
	}
}

func periodicFullGC(kubecli corev1client.CoreV1Interface, namespace string, d time.Duration) {
	gc := garbagecollection.New(kubecli)
	timer := time.NewTicker(d)
	defer timer.Stop()
	for {
		<-timer.C
		gc.FullyCollect(namespace)
	}
}

func startChaos(ctx context.Context, kubecli corev1client.CoreV1Interface, ns string, chaosLevel int) {
	m := chaos.NewMonkeys(kubecli)
	ls := labels.SelectorFromSet(map[string]string{"app": "nats"})

	switch chaosLevel {
	case 1:
		logrus.Info("chaos level = 1: randomly kill one NATS pod every 30 seconds at 50%")
		c := &chaos.CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:        rate.Every(30 * time.Second),
			KillProbability: 0.5,
			KillMax:         1,
		}
		go func() {
			time.Sleep(60 * time.Second) // don't start until quorum up
			m.CrushPods(ctx, c)
		}()

	case 2:
		logrus.Info("chaos level = 2: randomly kill at most five NATS pods every 30 seconds at 50%")
		c := &chaos.CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:        rate.Every(30 * time.Second),
			KillProbability: 0.5,
			KillMax:         5,
		}

		go m.CrushPods(ctx, c)

	default:
	}
}

func createRecorder(kubecli corev1client.CoreV1Interface, name, namespace string) record.EventRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(logrus.Infof)
	eventBroadcaster.StartRecordingToSink(&corev1client.EventSinkImpl{Interface: corev1client.New(kubecli.RESTClient()).Events(namespace)})
	return eventBroadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: name})
}
