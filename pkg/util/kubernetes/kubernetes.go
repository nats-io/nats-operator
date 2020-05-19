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
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	authenticationv1 "k8s.io/api/authentication/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8srand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp" // for gcp auth
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/nats-io/nats-operator/pkg/apis/nats/v1alpha2"
	natsclient "github.com/nats-io/nats-operator/pkg/client/clientset/versioned"
	natsalphav2client "github.com/nats-io/nats-operator/pkg/client/clientset/versioned/typed/nats/v1alpha2"
	natsconf "github.com/nats-io/nats-operator/pkg/conf"
	"github.com/nats-io/nats-operator/pkg/constants"
	"github.com/nats-io/nats-operator/pkg/util/retryutil"
	"github.com/nats-io/nats-operator/pkg/util/versionCheck"
)

const (
	TolerateUnreadyEndpointsAnnotation = "service.alpha.kubernetes.io/tolerate-unready-endpoints"
	versionAnnotationKey               = "nats.version"
)

const (
	LabelAppKey            = "app"
	LabelAppValue          = "nats"
	LabelClusterNameKey    = "nats_cluster"
	LabelClusterVersionKey = "nats_version"
)

func GetNATSVersion(pod *v1.Pod) string {
	return pod.Annotations[versionAnnotationKey]
}

func SetNATSVersion(pod *v1.Pod, version string) {
	pod.Annotations[versionAnnotationKey] = version
	pod.Labels[LabelClusterVersionKey] = version
}

func GetPodNames(pods []*v1.Pod) []string {
	if len(pods) == 0 {
		return nil
	}
	res := []string{}
	for _, p := range pods {
		res = append(res, p.Name)
	}
	return res
}

func MakeNATSImage(version string, serverImage string) string {
	return fmt.Sprintf("%s:%v", serverImage, version)
}

func PodWithNodeSelector(p *v1.Pod, ns map[string]string) *v1.Pod {
	p.Spec.NodeSelector = ns
	return p
}

func createService(kubecli corev1client.CoreV1Interface, svcName, clusterName, ns, clusterIP string, ports []v1.ServicePort, owner metav1.OwnerReference, selectors map[string]string, tolerateUnready bool) error {
	svc := newNatsServiceManifest(svcName, clusterName, clusterIP, ports, selectors, tolerateUnready)
	addOwnerRefToObject(svc.GetObjectMeta(), owner)
	_, err := kubecli.Services(ns).Create(svc)
	return err
}

// ClientServiceName returns the name of the client service based on the specified cluster name.
func ClientServiceName(clusterName string) string {
	return clusterName
}

func CreateClientService(kubecli corev1client.CoreV1Interface, clusterName, ns string, owner metav1.OwnerReference) error {
	ports := []v1.ServicePort{{
		Name:       "client",
		Port:       constants.ClientPort,
		TargetPort: intstr.FromInt(constants.ClientPort),
		Protocol:   v1.ProtocolTCP,
	}}
	selectors := LabelsForCluster(clusterName)
	return createService(kubecli, ClientServiceName(clusterName), clusterName, ns, "", ports, owner, selectors, false)
}

func ManagementServiceName(clusterName string) string {
	return clusterName + "-mgmt"
}

// CreateMgmtService creates an headless service for NATS management purposes.
func CreateMgmtService(kubecli corev1client.CoreV1Interface, clusterName, clusterVersion, ns string, owner metav1.OwnerReference) error {
	ports := []v1.ServicePort{
		{
			Name:       "cluster",
			Port:       constants.ClusterPort,
			TargetPort: intstr.FromInt(constants.ClusterPort),
			Protocol:   v1.ProtocolTCP,
		},
		{
			Name:       "monitoring",
			Port:       constants.MonitoringPort,
			TargetPort: intstr.FromInt(constants.MonitoringPort),
			Protocol:   v1.ProtocolTCP,
		},
		{
			Name:       "metrics",
			Port:       constants.MetricsPort,
			TargetPort: intstr.FromInt(constants.MetricsPort),
			Protocol:   v1.ProtocolTCP,
		},
	}
	selectors := LabelsForCluster(clusterName)
	selectors[LabelClusterVersionKey] = clusterVersion
	return createService(kubecli, ManagementServiceName(clusterName), clusterName, ns, v1.ClusterIPNone, ports, owner, selectors, true)
}

// addTLSConfig fills in the TLS configuration to be used in the config map.
func addTLSConfig(sconfig *natsconf.ServerConfig, cs v1alpha2.ClusterSpec) {
	if cs.TLS == nil {
		return
	}

	if cs.TLS.EnableHttps {
		// Replace monitoring port with https one.
		sconfig.HTTPSPort = int(constants.MonitoringPort)
		sconfig.HTTPPort = 0
	}

	if cs.TLS.ServerSecret != "" {
		sconfig.TLS = &natsconf.TLSConfig{
			CAFile:   constants.ServerCertsMountPath + "/" + cs.TLS.ServerSecretCAFileName,
			CertFile: constants.ServerCertsMountPath + "/" + cs.TLS.ServerSecretCertFileName,
			KeyFile:  constants.ServerCertsMountPath + "/" + cs.TLS.ServerSecretKeyFileName,
		}

		if cs.TLS.ClientsTLSTimeout > 0 {
			sconfig.TLS.Timeout = cs.TLS.ClientsTLSTimeout
		}

		// Verifying clients cert is disabled by default.
		sconfig.TLS.Verify = cs.TLS.Verify

		// Customize cipher suites and curve preferences.
		sconfig.TLS.CipherSuites = cs.TLS.CipherSuites
		sconfig.TLS.CurvePreferences = cs.TLS.CurvePreferences
	}
	if cs.TLS.RoutesSecret != "" {
		sconfig.Cluster.TLS = &natsconf.TLSConfig{
			CAFile:   filepath.Join(constants.RoutesCertsMountPath, cs.TLS.RoutesSecretCAFileName),
			CertFile: filepath.Join(constants.RoutesCertsMountPath, cs.TLS.RoutesSecretCertFileName),
			KeyFile:  filepath.Join(constants.RoutesCertsMountPath, cs.TLS.RoutesSecretKeyFileName),
		}
		if cs.TLS.RoutesTLSTimeout > 0 {
			sconfig.Cluster.TLS.Timeout = cs.TLS.RoutesTLSTimeout
		}
	}
	if cs.TLS.GatewaySecret != "" {
		sconfig.Gateway.TLS = &natsconf.TLSConfig{
			CAFile:   filepath.Join(constants.GatewayCertsMountPath, cs.TLS.GatewaySecretCAFileName),
			CertFile: filepath.Join(constants.GatewayCertsMountPath, cs.TLS.GatewaySecretCertFileName),
			KeyFile:  filepath.Join(constants.GatewayCertsMountPath, cs.TLS.GatewaySecretKeyFileName),
		}
		if cs.TLS.GatewaysTLSTimeout > 0 {
			sconfig.Gateway.TLS.Timeout = cs.TLS.GatewaysTLSTimeout
		}
	}
	if cs.TLS.LeafnodeSecret != "" {
		// Reuse the same settings as those for clients.
		sconfig.LeafNode.TLS = &natsconf.TLSConfig{
			CAFile:   filepath.Join(constants.LeafnodeCertsMountPath, cs.TLS.LeafnodeSecretCAFileName),
			CertFile: filepath.Join(constants.LeafnodeCertsMountPath, cs.TLS.LeafnodeSecretCertFileName),
			KeyFile:  filepath.Join(constants.LeafnodeCertsMountPath, cs.TLS.LeafnodeSecretKeyFileName),
		}
		timeout := cs.TLS.LeafnodesTLSTimeout
		if timeout > 0 {
			sconfig.LeafNode.TLS.Timeout = timeout
		}
	}
	if cs.Auth != nil && cs.Auth.TLSVerifyAndMap {
		sconfig.TLS.VerifyAndMap = true
	}
}

func addAuthConfig(
	kubecli corev1client.CoreV1Interface,
	operatorcli natsalphav2client.NatsV1alpha2Interface,
	ns string,
	clusterName string,
	sconfig *natsconf.ServerConfig,
	cs v1alpha2.ClusterSpec,
	owner metav1.OwnerReference,
) error {
	if cs.Auth == nil {
		return nil
	}

	if cs.Auth.EnableServiceAccounts {
		roleSelector := map[string]string{
			LabelClusterNameKey: clusterName,
		}

		users := make([]*natsconf.User, 0)
		roles, err := operatorcli.NatsServiceRoles(ns).List(metav1.ListOptions{
			LabelSelector: labels.SelectorFromSet(roleSelector).String(),
		})
		if err != nil {
			return err
		}

		namespaces, err := kubecli.Namespaces().List(metav1.ListOptions{})
		if err != nil {
			return err
		}

		for _, nsObj := range namespaces.Items {
			ns = nsObj.Name

			for _, role := range roles.Items {
				// Lookup for a ServiceAccount with the same name as the NatsServiceRole.

				sa, err := kubecli.ServiceAccounts(ns).Get(role.Name, metav1.GetOptions{})
				if err != nil {
					// TODO: Collect created secrets when the service account no
					// longer exists, currently only deleted when the NatsServiceRole
					// is deleted since it is the owner of the object.

					// Skip since cannot map unless valid service account is found.
					continue
				}

				// TODO: Add support for expiration of the issued tokens.
				tokenSecretName := fmt.Sprintf("%s-%s-bound-token", role.Name, clusterName)
				cs, err := kubecli.Secrets(ns).Get(tokenSecretName, metav1.GetOptions{})
				if err == nil {
					// We always get everything and apply, in case there is a diff
					// then the reloader will apply them.
					user := &natsconf.User{
						User:     role.Name,
						Password: string(cs.Data["token"]),
						Permissions: &natsconf.Permissions{
							Publish:   role.Spec.Permissions.Publish,
							Subscribe: role.Spec.Permissions.Subscribe,
						},
					}
					users = append(users, user)
					continue
				}

				// Create the secret, then make a service token request, and finally
				// update the secret with the token mapped to the service account.
				tokenSecret := &v1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:   tokenSecretName,
						Labels: LabelsForCluster(clusterName),
					},
				}

				// When the role that was mapped is deleted, then also delete the secret.
				addOwnerRefToObject(tokenSecret.GetObjectMeta(), role.AsOwner())
				tokenSecret, err = kubecli.Secrets(ns).Create(tokenSecret)
				if err != nil {
					return err
				}

				// Issue token with audience set for the NATS cluster in this namespace only,
				// this will prevent the token from being usable against the API Server.
				ar := &authenticationv1.TokenRequest{
					Spec: authenticationv1.TokenRequestSpec{
						Audiences: []string{fmt.Sprintf("nats://%s.%s.svc", clusterName, ns)},

						// Service Token will be valid for as long as the created secret exists.
						BoundObjectRef: &authenticationv1.BoundObjectReference{
							Kind:       "Secret",
							APIVersion: "v1",
							Name:       tokenSecret.Name,
							UID:        tokenSecret.UID,
						},
					},
				}
				tr, err := kubecli.ServiceAccounts(ns).CreateToken(sa.Name, ar)
				if err != nil {
					return err
				}

				if err == nil {
					// Update secret with issued token, then save the user in the NATS Config.
					token := tr.Status.Token
					tokenSecret.Data = map[string][]byte{
						"token": []byte(token),
					}
					tokenSecret, err = kubecli.Secrets(ns).Update(tokenSecret)
					if err != nil {
						return err
					}
					user := &natsconf.User{
						User:     role.Name,
						Password: string(token),
						Permissions: &natsconf.Permissions{
							Publish:   role.Spec.Permissions.Publish,
							Subscribe: role.Spec.Permissions.Subscribe,
						},
					}
					users = append(users, user)
				}
			}
		}
		// Expand authorization rules from the service account tokens.
		sconfig.Authorization = &natsconf.AuthorizationConfig{
			Users: users,
		}
		return nil
	} else if cs.Auth.ClientsAuthSecret != "" {
		// Authorization implementation using a secret with the explicit
		// configuration of all the accounts from a cluster, cannot be
		// used together with service accounts.
		result, err := kubecli.Secrets(ns).Get(cs.Auth.ClientsAuthSecret, metav1.GetOptions{})
		if err != nil {
			return err
		}

		var clientAuth *natsconf.AuthorizationConfig
		for _, v := range result.Data {
			err := json.Unmarshal(v, &clientAuth)
			if err != nil {
				return err
			}
			if cs.Auth.ClientsAuthTimeout > 0 {
				clientAuth.Timeout = cs.Auth.ClientsAuthTimeout
			}
			sconfig.Authorization = clientAuth
			break
		}
		return nil
	} else if cs.Auth.ClientsAuthFile != "" {
		sconfig.Authorization = &natsconf.AuthorizationConfig{
			Include: cs.Auth.ClientsAuthFile,
		}
		return nil
	}
	return nil
}

func addGatewayConfig(sconfig *natsconf.ServerConfig, cluster v1alpha2.ClusterSpec) {
	gateways := make([]*natsconf.RemoteGatewayOpts, 0)

	for _, gw := range cluster.GatewayConfig.Gateways {
		sgw := &natsconf.RemoteGatewayOpts{
			Name: gw.Name,
			URL:  gw.URL,
		}
		gateways = append(gateways, sgw)
	}

	sconfig.Gateway = &natsconf.GatewayConfig{
		Name:          cluster.GatewayConfig.Name,
		Port:          cluster.GatewayConfig.Port,
		Gateways:      gateways,
		RejectUnknown: cluster.GatewayConfig.RejectUnknown,
		Include:       filepath.Join(".", constants.BootConfigGatewayFilePath),
	}

	// Add the same for leaf nodes if present
	if cluster.LeafNodeConfig != nil {
		sconfig.LeafNode = &natsconf.LeafNodeServerConfig{
			Port:    cluster.LeafNodeConfig.Port,
			Include: "./advertise/gateway_advertise.conf",
		}
	}
	return
}

// addOperatorConfig fills in the operator configuration to be used in the config map.
func addOperatorConfig(sconfig *natsconf.ServerConfig, cs v1alpha2.ClusterSpec) {
	if cs.OperatorConfig == nil {
		return
	}
	sconfig.JWT = filepath.Join(constants.OperatorJWTMountPath, constants.DefaultOperatorJWTFileName)
	sconfig.SystemAccount = cs.OperatorConfig.SystemAccount
	sconfig.Resolver = cs.OperatorConfig.Resolver
}

// CreateAndWaitPod is an util for testing.
// We should eventually get rid of this in critical code path and move it to test util.
func CreateAndWaitPod(kubecli corev1client.CoreV1Interface, ns string, pod *v1.Pod, timeout time.Duration) (*v1.Pod, error) {
	_, err := kubecli.Pods(ns).Create(pod)
	if err != nil {
		return nil, err
	}

	interval := 5 * time.Second
	var retPod *v1.Pod
	err = retryutil.Retry(interval, int(timeout/(interval)), func() (bool, error) {
		retPod, err = kubecli.Pods(ns).Get(pod.Name, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		switch retPod.Status.Phase {
		case v1.PodRunning:
			return true, nil
		case v1.PodPending:
			return false, nil
		default:
			return false, fmt.Errorf("unexpected pod status.phase: %v", retPod.Status.Phase)
		}
	})

	if err != nil {
		if retryutil.IsRetryFailure(err) {
			return nil, fmt.Errorf("failed to wait pod running, it is still pending: %v", err)
		}
		return nil, fmt.Errorf("failed to wait pod running: %v", err)
	}

	return retPod, nil
}

// ConfigSecret returns the name of the secret that contains the configuration for the NATS cluster with the specified name.
func ConfigSecret(clusterName string) string {
	return clusterName
}

// CreateConfigSecret creates the secret that contains the configuration file for a given NATS cluster..
func CreateConfigSecret(kubecli corev1client.CoreV1Interface, operatorcli natsalphav2client.NatsV1alpha2Interface, clusterName, ns string, cluster v1alpha2.ClusterSpec, owner metav1.OwnerReference) error {
	sconfig := &natsconf.ServerConfig{
		Port:     int(constants.ClientPort),
		HTTPPort: int(constants.MonitoringPort),
		Cluster: &natsconf.ClusterConfig{
			Port: int(constants.ClusterPort),
		},
	}

	if cluster.ExtraRoutes != nil {
		routes := make([]string, 0)
		for _, extraCluster := range cluster.ExtraRoutes {
			switch {
			case extraCluster.Route != "":
				// If route is explicit just include as is.
				routes = append(routes, extraCluster.Route)
			case extraCluster.Cluster != "":
				route := fmt.Sprintf("nats://%s:%d",
					ManagementServiceName(extraCluster.Cluster),
					constants.ClusterPort)
				routes = append(routes, route)
			}
		}
		sconfig.Cluster.Routes = routes
	}
	if cluster.UseServerName {
		sconfig.ServerName = "$SERVER_NAME"
	}

	addConfig(sconfig, cluster)
	err := addAuthConfig(kubecli, operatorcli, ns, clusterName, sconfig, cluster, owner)
	if err != nil {
		return err
	}
	rawConfig, err := natsconf.Marshal(sconfig)
	if err != nil {
		return err
	}
	if cluster.UseServerName {
		rawConfig = bytes.Replace(rawConfig, []byte(`"$SERVER_NAME"`), []byte("$SERVER_NAME"), -1)
	}

	// FIXME: Quoted "include" causes include to be ignored.
	rawConfig = bytes.Replace(rawConfig, []byte(`"include":`), []byte("include "), -1)

	labels := LabelsForCluster(clusterName)
	cm := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   ConfigSecret(clusterName),
			Labels: labels,
		},
		Data: map[string][]byte{
			constants.ConfigFileName: rawConfig,
		},
	}
	addOwnerRefToObject(cm.GetObjectMeta(), owner)

	_, err = kubecli.Secrets(ns).Create(cm)
	if apierrors.IsAlreadyExists(err) {
		// Skip in case it was created already and update instead
		// with the latest configuration.
		_, err = kubecli.Secrets(ns).Update(cm)
		return err
	}

	return nil
}

// UpdateConfigSecret applies the new configuration of the cluster,
// such as modifying the routes available in the cluster.
func UpdateConfigSecret(
	kubecli corev1client.CoreV1Interface,
	operatorcli natsalphav2client.NatsV1alpha2Interface,
	clusterName, ns string,
	cluster v1alpha2.ClusterSpec,
	owner metav1.OwnerReference,
) error {
	// List all available pods then generate the routes
	// for the NATS cluster.
	routes := make([]string, 0)
	podList, err := kubecli.Pods(ns).List(ClusterListOpt(clusterName))
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		// Skip pods that have failed
		switch pod.Status.Phase {
		case "Failed":
			continue
		}

		route := fmt.Sprintf("nats://%s.%s.%s.svc:%d",
			pod.Name, ManagementServiceName(clusterName), ns, constants.ClusterPort)
		routes = append(routes, route)
	}

	if cluster.ExtraRoutes != nil {
		for _, extraCluster := range cluster.ExtraRoutes {
			switch {
			case extraCluster.Route != "":
				// If route is explicit just include as is.
				routes = append(routes, extraCluster.Route)
			case extraCluster.Cluster != "":
				route := fmt.Sprintf("nats://%s:%d",
					ManagementServiceName(extraCluster.Cluster),
					constants.ClusterPort)
				routes = append(routes, route)
			}
		}
	}

	sconfig := &natsconf.ServerConfig{
		Port:     int(constants.ClientPort),
		HTTPPort: int(constants.MonitoringPort),
		Cluster: &natsconf.ClusterConfig{
			Port:   int(constants.ClusterPort),
			Routes: routes,
		},
	}
	if cluster.UseServerName {
		sconfig.ServerName = "$SERVER_NAME"
	}

	addConfig(sconfig, cluster)
	err = addAuthConfig(kubecli, operatorcli, ns, clusterName, sconfig, cluster, owner)
	if err != nil {
		return err
	}
	rawConfig, err := natsconf.Marshal(sconfig)
	if err != nil {
		return err
	}

	// FIXME: Quoted "include" causes include to be ignored.
	rawConfig = bytes.Replace(rawConfig, []byte(`"include":`), []byte("include "), -1)

	// Replace server name so that it is unquoted and evaled as an env var.
	if cluster.UseServerName {
		rawConfig = bytes.Replace(rawConfig, []byte(`"$SERVER_NAME"`), []byte("$SERVER_NAME"), -1)
	}

	cm, err := kubecli.Secrets(ns).Get(clusterName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	// Make sure that the secret has the required labels.
	if cm.Labels == nil {
		cm.Labels = make(map[string]string)
	}
	for key, val := range LabelsForCluster(clusterName) {
		cm.Labels[key] = val
	}
	// Update the configuration.
	cm.Data[constants.ConfigFileName] = rawConfig

	_, err = kubecli.Secrets(ns).Update(cm)
	return err
}

func addConfig(sconfig *natsconf.ServerConfig, cluster v1alpha2.ClusterSpec) {
	if cluster.ServerConfig != nil {
		sconfig.Debug = cluster.ServerConfig.Debug
		sconfig.Trace = cluster.ServerConfig.Trace
		sconfig.WriteDeadline = cluster.ServerConfig.WriteDeadline
		sconfig.MaxConnections = cluster.ServerConfig.MaxConnections
		sconfig.MaxPayload = cluster.ServerConfig.MaxPayload
		sconfig.MaxPending = cluster.ServerConfig.MaxPending
		sconfig.MaxSubscriptions = cluster.ServerConfig.MaxSubscriptions
		sconfig.MaxControlLine = cluster.ServerConfig.MaxControlLine
		sconfig.Logtime = !cluster.ServerConfig.DisableLogtime
	} else {
		sconfig.Logtime = true
	}

	// Observe .spec.lameDuckDurationSeconds if specified.
	if cluster.LameDuckDurationSeconds != nil {
		sconfig.LameDuckDuration = fmt.Sprintf("%ds", *cluster.LameDuckDurationSeconds)
	}
	if cluster.Pod != nil && cluster.Pod.AdvertiseExternalIP {
		sconfig.Include = filepath.Join(".", constants.BootConfigFilePath)
	}
	if cluster.GatewayConfig != nil {
		addGatewayConfig(sconfig, cluster)
	}
	addTLSConfig(sconfig, cluster)
	addOperatorConfig(sconfig, cluster)
}

func newNatsConfigMapVolume(clusterName string) v1.Volume {
	return v1.Volume{
		Name: constants.ConfigMapVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: clusterName,
			},
		},
	}
}

func newNatsConfigMapVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.ConfigMapVolumeName,
		MountPath: constants.ConfigMapMountPath,
	}
}

func newNatsPidFileVolume() v1.Volume {
	return v1.Volume{
		Name: constants.PidFileVolumeName,
		VolumeSource: v1.VolumeSource{
			EmptyDir: &v1.EmptyDirVolumeSource{},
		},
	}
}

func newNatsPidFileVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.PidFileVolumeName,
		MountPath: constants.PidFileMountPath,
	}
}

func newNatsServiceManifest(svcName, clusterName, clusterIP string, ports []v1.ServicePort, selectors map[string]string, tolerateUnready bool) *v1.Service {
	labels := map[string]string{
		LabelAppKey:         LabelAppValue,
		LabelClusterNameKey: clusterName,
	}

	annotations := make(map[string]string)
	if tolerateUnready == true {
		annotations[TolerateUnreadyEndpointsAnnotation] = "true"
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:        svcName,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: v1.ServiceSpec{
			Ports:     ports,
			Selector:  selectors,
			ClusterIP: clusterIP,
		},
	}
	return svc
}

func newNatsServerSecretVolume(secretName string) v1.Volume {
	return v1.Volume{
		Name: constants.ServerSecretVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

func newNatsServerSecretVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.ServerSecretVolumeName,
		MountPath: constants.ServerCertsMountPath,
	}
}

func newNatsRoutesSecretVolume(secretName string) v1.Volume {
	return v1.Volume{
		Name: constants.RoutesSecretVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

func newNatsRoutesSecretVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.RoutesSecretVolumeName,
		MountPath: constants.RoutesCertsMountPath,
	}
}

func newNatsGatewaySecretVolume(secretName string) v1.Volume {
	return v1.Volume{
		Name: constants.GatewaySecretVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

func newNatsGatewaySecretVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.GatewaySecretVolumeName,
		MountPath: constants.GatewayCertsMountPath,
	}
}

func newNatsLeafnodeSecretVolume(secretName string) v1.Volume {
	return v1.Volume{
		Name: constants.LeafnodeSecretVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

func newNatsLeafnodeSecretVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.LeafnodeSecretVolumeName,
		MountPath: constants.LeafnodeCertsMountPath,
	}
}

func newNatsOperatorJWTSecretVolume(secretName string) v1.Volume {
	return v1.Volume{
		Name: constants.OperatorJWTVolumeName,
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	}
}

func newNatsOperatorJWTSecretVolumeMount() v1.VolumeMount {
	return v1.VolumeMount{
		Name:      constants.OperatorJWTVolumeName,
		MountPath: constants.OperatorJWTMountPath,
	}
}

func addOwnerRefToObject(o metav1.Object, r metav1.OwnerReference) {
	o.SetOwnerReferences(append(o.GetOwnerReferences(), r))
}

// NewNatsPodSpec returns a NATS peer pod specification, based on the cluster specification.
func NewNatsPodSpec(namespace, name, clusterName string, cs v1alpha2.ClusterSpec, owner metav1.OwnerReference) *v1.Pod {
	var (
		enableClientsHostPort bool
		annotations           = map[string]string{}
		containers            = make([]v1.Container, 0)
		volumes               = make([]v1.Volume, 0)
		volumeMounts          = make([]v1.VolumeMount, 0)
		labels                = map[string]string{
			LabelAppKey:            "nats",
			LabelClusterNameKey:    clusterName,
			LabelClusterVersionKey: cs.Version,
		}
	)

	// ConfigMap: Volume declaration for the Pod and Container.
	volume := newNatsConfigMapVolume(clusterName)
	volumes = append(volumes, volume)
	volumeMount := newNatsConfigMapVolumeMount()
	volumeMounts = append(volumeMounts, volumeMount)

	// Extra mount to share the pid file from server
	volume = newNatsPidFileVolume()
	volumes = append(volumes, volume)
	volumeMount = newNatsPidFileVolumeMount()
	volumeMounts = append(volumeMounts, volumeMount)

	if cs.Pod != nil {
		// User supplied volumes and mounts
		volumeMounts = append(volumeMounts, cs.Pod.VolumeMounts...)
		enableClientsHostPort = cs.Pod.EnableClientsHostPort
	}

	var gatewayPort int
	if cs.GatewayConfig != nil {
		gatewayPort = cs.GatewayConfig.Port
	}
	var leafnodePort int
	if cs.LeafNodeConfig != nil {
		leafnodePort = cs.LeafNodeConfig.Port
	}

	// Initialize the pod spec with a template in case it is present.
	spec := &v1.PodSpec{}
	if cs.PodTemplate != nil {
		spec = cs.PodTemplate.Spec.DeepCopy()
		if spec.Containers != nil && len(spec.Containers) > 0 {
			containers = spec.Containers
		}
	}

	// First container has to be the NATS container
	var container v1.Container
	if len(spec.Containers) > 0 {
		container = spec.Containers[0]
	} else {
		container = v1.Container{}
	}

	container = natsPodContainer(container, clusterName, cs.Version, cs.ServerImage,
		enableClientsHostPort, gatewayPort, leafnodePort)
	container = containerWithLivenessProbe(container, natsLivenessProbe(cs))

	// In case TLS was enabled as part of the NATS cluster
	// configuration then should include the configuration here.
	if cs.TLS != nil {
		if cs.TLS.ServerSecret != "" {
			volume = newNatsServerSecretVolume(cs.TLS.ServerSecret)
			volumes = append(volumes, volume)

			volumeMount := newNatsServerSecretVolumeMount()
			volumeMounts = append(volumeMounts, volumeMount)
		}

		if cs.TLS.RoutesSecret != "" {
			volume = newNatsRoutesSecretVolume(cs.TLS.RoutesSecret)
			volumes = append(volumes, volume)

			volumeMount := newNatsRoutesSecretVolumeMount()
			volumeMounts = append(volumeMounts, volumeMount)
		}
		if cs.TLS.GatewaySecret != "" {
			volume = newNatsGatewaySecretVolume(cs.TLS.GatewaySecret)
			volumes = append(volumes, volume)

			volumeMount := newNatsGatewaySecretVolumeMount()
			volumeMounts = append(volumeMounts, volumeMount)
		}
		if cs.TLS.LeafnodeSecret != "" {
			volume = newNatsLeafnodeSecretVolume(cs.TLS.LeafnodeSecret)
			volumes = append(volumes, volume)

			volumeMount := newNatsLeafnodeSecretVolumeMount()
			volumeMounts = append(volumeMounts, volumeMount)
		}
	}

	if cs.OperatorConfig != nil {
		volume = newNatsOperatorJWTSecretVolume(cs.OperatorConfig.Secret)
		volumes = append(volumes, volume)

		volumeMount := newNatsOperatorJWTSecretVolumeMount()
		volumeMounts = append(volumeMounts, volumeMount)
	}

	// Configure initializer container to resolve the external ip
	// from the pod.
	var (
		advertiseExternalIP bool = cs.Pod != nil && cs.Pod.AdvertiseExternalIP
		bootconfig          v1.Container
	)
	if advertiseExternalIP {
		var (
			bootconfigImage    = constants.DefaultBootconfigImage
			bootconfigImageTag = constants.DefaultBootconfigImageTag
		)
		if cs.Pod.BootConfigContainerImage != "" {
			bootconfigImage = cs.Pod.BootConfigContainerImage
		}
		if cs.Pod.BootConfigContainerImageTag != "" {
			bootconfigImageTag = cs.Pod.BootConfigContainerImageTag
		}
		image := fmt.Sprintf("%s:%s", bootconfigImage, bootconfigImageTag)
		bootconfig = v1.Container{
			Name:  "bootconfig",
			Image: image,
		}
		bootconfig.Env = []v1.EnvVar{
			{
				Name: "KUBERNETES_NODE_NAME",
				ValueFrom: &v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector{
						FieldPath: "spec.nodeName",
					},
				},
			},
		}

		// Add the empty directory mount for the pod, nats
		// container and init container
		mount := v1.VolumeMount{
			Name:      "advertiseconfig",
			MountPath: "/etc/nats-config/advertise",
			SubPath:   "advertise",
		}
		bootconfig.VolumeMounts = []v1.VolumeMount{mount}
		volumeMounts = append(volumeMounts, mount)

		volume := v1.Volume{
			Name: "advertiseconfig",
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		}
		volumes = append(volumes, volume)

		bootconfig.Command = []string{
			"nats-pod-bootconfig",
			"-f", filepath.Join(constants.ConfigMapMountPath, constants.BootConfigFilePath),
			"-gf", filepath.Join(constants.ConfigMapMountPath, constants.BootConfigGatewayFilePath),
		}
	}
	container.VolumeMounts = volumeMounts

	if cs.Pod != nil {
		container = containerWithRequirements(container, cs.Pod.Resources)
	}

	// Grab the A record that will correspond to the current pod
	// so we can use it as the cluster advertise host.
	// This helps with avoiding route connection errors in TLS-enabled clusters.
	advertiseHost := fmt.Sprintf("%s.%s.%s.svc", name, ManagementServiceName(clusterName), namespace)

	// Rely on the shared configuration map for configuring the cluster.
	retries := strconv.Itoa(constants.ConnectRetries)
	cmd := []string{
		versionCheck.ServerBinaryPath(cs.Version),
		"-c",
		constants.ConfigFilePath,
		"-P",
		constants.PidFilePath,
		"--cluster_advertise",
		advertiseHost,
		"--connect_retries",
		retries,
	}
	if cs.NoAdvertise {
		cmd = append(cmd, "--no_advertise")
	}

	container.Command = cmd

	// If there were containers defined already, then replace the NATS container.
	if len(containers) > 0 {
		containers[0] = container
	} else {
		containers = append(containers, container)
	}

	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Labels:      labels,
			Annotations: annotations,
		},
	}
	pod.Spec = *spec

	// Required overrides.
	pod.Spec.Hostname = name
	pod.Spec.Subdomain = ManagementServiceName(clusterName)

	// Set default restart policy
	if pod.Spec.RestartPolicy == "" {
		pod.Spec.RestartPolicy = v1.RestartPolicyAlways
	}

	if advertiseExternalIP {
		pod.Spec.InitContainers = append(pod.Spec.InitContainers, bootconfig)
	}

	// Enable PID namespace sharing and attach sidecar that
	// reloads the server whenever the config file is updated.
	if cs.Pod != nil && cs.Pod.EnableConfigReload {
		pod.Spec.ShareProcessNamespace = &[]bool{true}[0]

		// Allow customizing reloader image
		image := constants.DefaultReloaderImage
		imageTag := constants.DefaultReloaderImageTag
		imagePullPolicy := constants.DefaultReloaderImagePullPolicy
		if cs.Pod.ReloaderImage != "" {
			image = cs.Pod.ReloaderImage
		}
		if cs.Pod.ReloaderImageTag != "" {
			imageTag = cs.Pod.ReloaderImageTag
		}
		if cs.Pod.ReloaderImagePullPolicy != "" {
			imagePullPolicy = cs.Pod.ReloaderImagePullPolicy
		}

		authFilePath := ""
		if cs.Auth != nil {
			authFilePath = cs.Auth.ClientsAuthFile
		}

		reloaderContainer := natsPodReloaderContainer(image, imageTag, imagePullPolicy, authFilePath, cs.Pod.ReloaderResources)
		reloaderContainer.VolumeMounts = volumeMounts
		containers = append(containers, reloaderContainer)
	}

	if cs.Pod != nil && cs.Pod.EnableMetrics {
		// Add pod annotations for prometheus metrics
		pod.ObjectMeta.Annotations["prometheus.io/scrape"] = "true"
		pod.ObjectMeta.Annotations["prometheus.io/port"] = strconv.Itoa(constants.MetricsPort)

		// Allow customizing promethues metrics exporter image
		image := constants.DefaultMetricsImage
		imageTag := constants.DefaultMetricsImageTag
		imagePullPolicy := constants.DefaultMetricsImagePullPolicy
		if cs.Pod.MetricsImage != "" {
			image = cs.Pod.MetricsImage
		}
		if cs.Pod.MetricsImageTag != "" {
			imageTag = cs.Pod.MetricsImageTag
		}
		if cs.Pod.MetricsImagePullPolicy != "" {
			imagePullPolicy = cs.Pod.MetricsImagePullPolicy
		}

		metricsContainer := natsPodMetricsContainer(image, imageTag, imagePullPolicy)
		containers = append(containers, metricsContainer)
	}

	// pod.Spec.Containers = append(pod.Spec.Containers, containers...)
	pod.Spec.Containers = containers
	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)

	applyPodPolicy(clusterName, pod, cs.Pod)

	SetNATSVersion(pod, cs.Version)

	addOwnerRefToObject(pod.GetObjectMeta(), owner)

	return pod
}

// MustNewKubeConfig builds a configuration object by either reading from the specified kubeconfig file or by using an in-cluster config.
func MustNewKubeConfig(kubeconfig string) *rest.Config {
	var (
		cfg *rest.Config
		err error
	)
	if len(kubeconfig) == 0 {
		cfg, err = InClusterConfig()
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	}
	if err != nil {
		panic(err)
	}
	return cfg
}

// MustNewKubeClientFromConfig builds a Kubernetes client based on the specified configuration object.
func MustNewKubeClientFromConfig(cfg *rest.Config) kubernetes.Interface {
	return kubernetes.NewForConfigOrDie(cfg)
}

// MustNewNatsClientFromConfig builds a client for our API based on the specified configuration object.
func MustNewNatsClientFromConfig(cfg *rest.Config) natsclient.Interface {
	return natsclient.NewForConfigOrDie(cfg)
}

func InClusterConfig() (*rest.Config, error) {
	// Work around https://github.com/kubernetes/kubernetes/issues/40973
	if len(os.Getenv("KUBERNETES_SERVICE_HOST")) == 0 {
		addrs, err := net.LookupHost("kubernetes.default.svc")
		if err != nil {
			panic(err)
		}
		os.Setenv("KUBERNETES_SERVICE_HOST", addrs[0])
	}
	if len(os.Getenv("KUBERNETES_SERVICE_PORT")) == 0 {
		os.Setenv("KUBERNETES_SERVICE_PORT", "443")
	}
	return rest.InClusterConfig()
}

func IsKubernetesResourceAlreadyExistError(err error) bool {
	return apierrors.IsAlreadyExists(err)
}

func IsKubernetesResourceNotFoundError(err error) bool {
	return apierrors.IsNotFound(err)
}

// We are using internal api types for cluster related.
func ClusterListOpt(clusterName string) metav1.ListOptions {
	return metav1.ListOptions{
		LabelSelector: LabelSelectorForCluster(clusterName).String(),
	}
}

// LabelSelectorForCluster returns a label selector that matches resources belonging to the NATS cluster with the specified name.
func LabelSelectorForCluster(clusterName string) labels.Selector {
	return labels.SelectorFromSet(LabelsForCluster(clusterName))
}

// NatsServiceRoleLabelSelectorForCuster returns a label selector that matches NatsServiceRole resources referencing the NATS cluster with the specified name.
func NatsServiceRoleLabelSelectorForCluster(clusterName string) labels.Selector {
	return labels.SelectorFromSet(map[string]string{
		LabelClusterNameKey: clusterName,
	})
}

func LabelsForCluster(clusterName string) map[string]string {
	return map[string]string{
		LabelAppKey:         LabelAppValue,
		LabelClusterNameKey: clusterName,
	}
}

func CreatePatch(o, n, datastruct interface{}) ([]byte, error) {
	oldData, err := json.Marshal(o)
	if err != nil {
		return nil, err
	}
	newData, err := json.Marshal(n)
	if err != nil {
		return nil, err
	}
	return strategicpatch.CreateTwoWayMergePatch(oldData, newData, datastruct)
}

// mergeMaps merges l2 into l1. Conflicting keys will be skipped.
func mergeMaps(l1, l2 map[string]string) {
	for k, v := range l2 {
		if _, ok := l1[k]; ok {
			continue
		}
		l1[k] = v
	}
}

// UniquePodName generates a unique name for the Pod.
func UniquePodName() string {
	return fmt.Sprintf("nats-%s", k8srand.String(10))
}

// ResourceKey returns the "namespace/name" key that represents the specified resource.
func ResourceKey(obj interface{}) string {
	res, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil {
		return "(unknown)"
	}
	return res
}
