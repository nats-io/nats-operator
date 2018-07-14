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
	"time"

	"github.com/nats-io/nats-operator/pkg/chaos"
	"github.com/nats-io/nats-operator/pkg/client"
	"github.com/nats-io/nats-operator/pkg/controller"
	"github.com/nats-io/nats-operator/pkg/debug"
	"github.com/nats-io/nats-operator/pkg/debug/local"
	"github.com/nats-io/nats-operator/pkg/garbagecollection"
	kubernetesutil "github.com/nats-io/nats-operator/pkg/util/kubernetes"
	"github.com/nats-io/nats-operator/pkg/util/probe"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"
	"github.com/nats-io/nats-operator/version"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes/scheme"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/tools/record"
)

var (
	namespace  string
	name       string
	listenAddr string
	gcInterval time.Duration

	chaosLevel int

	printVersion bool
)

func init() {
	flag.StringVar(&debug.DebugFilePath, "debug-logfile-path", "", "only for a self hosted cluster, the path where the debug logfile will be written, recommended to be under: /var/tmp/nats-operator/debug/ to avoid any issue with lack of write permissions")
	flag.StringVar(&local.KubeConfigPath, "debug-kube-config-path", "", "the path to the local 'kubectl' config file (only for local debugging)")
	flag.StringVar(&local.ServiceAccountName, "debug-service-account-name", "default", "the name of the service account which to use (only for local debugging)")
	flag.StringVar(&local.PodName, "debug-pod-name", "nats-operator-debug", "the name of the pod which to report to EventRecorder (only for local debugging).")

	flag.StringVar(&listenAddr, "listen-addr", "0.0.0.0:8080", "The address on which the HTTP server will listen to")
	// chaos level will be removed once we have a formal tool to inject failures.
	flag.IntVar(&chaosLevel, "chaos-level", -1, "DO NOT USE IN PRODUCTION - level of chaos injected into the nats clusters created by the operator.")
	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
	flag.DurationVar(&gcInterval, "gc-interval", 10*time.Minute, "GC interval")
	flag.Parse()

	// TODO: remove this and use CR client

	var (
		cfg *rest.Config
		err error
	)

	if len(local.KubeConfigPath) == 0 {
		cfg, err = kubernetesutil.InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", local.KubeConfigPath)
	}

	if err != nil {
		panic(err)
	}

	controller.MasterHost = cfg.Host
	restcli, _, err := client.New(cfg)
	if err != nil {
		panic(err)
	}
	controller.KubeHttpCli = restcli.Client
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

	if len(local.KubeConfigPath) == 0 {
		name = os.Getenv("MY_POD_NAME")
	} else {
		name = local.PodName
	}
	if len(name) == 0 {
		logrus.Fatalf("must set env MY_POD_NAME (or --debug-pod-name for local debugging)")
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c)
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

	id, err := os.Hostname()
	if err != nil {
		logrus.Fatalf("failed to get hostname: %v", err)
	}

	kubecli := kubernetesutil.MustNewKubeClient()

	http.HandleFunc(probe.HTTPReadyzEndpoint, probe.ReadyzHandler)
	go http.ListenAndServe(listenAddr, nil)

	rl, err := resourcelock.New(resourcelock.EndpointsResourceLock,
		namespace,
		"nats-operator",
		kubecli,
		resourcelock.ResourceLockConfig{
			Identity:      id,
			EventRecorder: createRecorder(kubecli, name, namespace),
		})
	if err != nil {
		logrus.Fatalf("error creating lock: %v", err)
	}

	leaderelection.RunOrDie(context.TODO(), leaderelection.LeaderElectionConfig{
		Lock:          rl,
		LeaseDuration: 15 * time.Second,
		RenewDeadline: 10 * time.Second,
		RetryPeriod:   2 * time.Second,
		Callbacks: leaderelection.LeaderCallbacks{
			OnStartedLeading: run,
			OnStoppedLeading: func() {
				logrus.Fatalf("leader election lost")
			},
		},
	})

	panic("unreachable")
}

func run(ctx context.Context) {
	cfg := newControllerConfig()
	if err := cfg.Validate(); err != nil {
		logrus.Fatalf("invalid operator config: %v", err)
	}

	go periodicFullGC(cfg.KubeCli, cfg.Namespace, gcInterval)

	startChaos(context.Background(), cfg.KubeCli, cfg.Namespace, chaosLevel)

	for {
		c := controller.New(cfg)
		err := c.Run(ctx)
		switch err {
		case controller.ErrVersionOutdated:
		default:
			logrus.Fatalf("controller Run() ended with failure: %v", err)
		}
	}
}

func newControllerConfig() controller.Config {
	kubecli := kubernetesutil.MustNewKubeClient()

	var (
		err            error
		serviceAccount string
	)

	if len(local.KubeConfigPath) == 0 {
		serviceAccount, err = getMyPodServiceAccount(kubecli)
		if err != nil {
			logrus.Fatalf("fail to get my pod's service account: %v", err)
		}
	} else {
		serviceAccount = local.ServiceAccountName
		if len(serviceAccount) == 0 {
			logrus.Fatalf("invalid service account name specified")
		}
	}
	cfg := controller.Config{
		Namespace:      namespace,
		ServiceAccount: serviceAccount,
		KubeCli:        kubecli,
		KubeExtCli:     kubernetesutil.MustNewKubeExtClient(),
		OperatorCli:    kubernetesutil.MustNewOperatorClient(),
	}

	return cfg
}

func getMyPodServiceAccount(kubecli corev1client.CoreV1Interface) (string, error) {
	var sa string
	err := retryutil.Retry(5*time.Second, 100, func() (bool, error) {
		pod, err := kubecli.Pods(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			logrus.Errorf("fail to get operator pod (%s): %v", name, err)
			return false, nil
		}
		sa = pod.Spec.ServiceAccountName
		return true, nil
	})
	return sa, err
}

func periodicFullGC(kubecli corev1client.CoreV1Interface, ns string, d time.Duration) {
	gc := garbagecollection.New(kubecli, ns)
	timer := time.NewTicker(d)
	defer timer.Stop()
	for {
		<-timer.C
		err := gc.FullyCollect()
		if err != nil {
			logrus.Warningf("failed to cleanup resources: %v", err)
		}
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
