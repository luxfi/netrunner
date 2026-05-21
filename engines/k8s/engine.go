// Copyright (C) 2025, Lux Industries Inc. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

//go:build k8s

// Package k8s provides a Kubernetes-based deployment engine for Lux networks.
// It implements the Engine interface to deploy validator nodes as StatefulSets.
//
// Gated behind -tags k8s. k8s.io/client-go transitively imports
// google.golang.org/protobuf via gnostic-models; the project rule
// (ZAP internal, no proto on the wire) means we keep that out of the
// default netrunner binary. Build with `go build -tags k8s ./...` when
// you need Kubernetes deployment.
package k8s

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/luxfi/ids"
	"github.com/luxfi/netrunner/engines"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

func init() {
	engines.Register(EngineK8s, NewEngine)
}

const (
	EngineK8s engines.EngineType = "k8s"

	// Default resource requests/limits
	defaultMemoryRequest = "4Gi"
	defaultMemoryLimit   = "8Gi"
	defaultCPURequest    = "2"
	defaultCPULimit      = "4"
	defaultStorageSize   = "100Gi"

	// Labels
	labelApp     = "app"
	labelNetwork = "lux-network"
	labelNode    = "lux-node"
)

// Engine implements the Engine interface for Kubernetes deployments
type Engine struct {
	mu          sync.RWMutex
	name        string
	namespace   string
	image       string
	client      *kubernetes.Clientset
	networkID   uint32
	chainID     ids.ID
	replicas    int32
	startTime   time.Time
	running     bool
	httpPort    uint16
	stakingPort uint16

	// K8s-specific options
	storageClass string
	nodeSelector map[string]string
}

// NewEngine creates a new K8s engine
func NewEngine(name string, image string) (engines.Engine, error) {
	// Load kubeconfig from default location
	config, err := clientcmd.BuildConfigFromFlags("", clientcmd.RecommendedHomeFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig: %w", err)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create k8s client: %w", err)
	}

	// Default to ghcr.io/luxfi/node:latest if not specified
	if image == "" {
		image = "ghcr.io/luxfi/node:latest"
	}

	return &Engine{
		name:         name,
		namespace:    "lux-" + name,
		image:        image,
		client:       client,
		replicas:     5, // Default 5 validators
		storageClass: "do-block-storage",
	}, nil
}

// Name returns the engine name
func (e *Engine) Name() string {
	return e.name
}

// Type returns the engine type
func (e *Engine) Type() engines.EngineType {
	return EngineK8s
}

// Start deploys the Lux network to Kubernetes
func (e *Engine) Start(ctx context.Context, config *engines.NodeConfig) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("engine %s already running", e.name)
	}

	e.networkID = config.NetworkID
	e.httpPort = config.HTTPPort
	e.stakingPort = config.StakingPort

	// Create namespace
	if err := e.createNamespace(ctx); err != nil {
		return fmt.Errorf("failed to create namespace: %w", err)
	}

	// Create ConfigMap with genesis
	if err := e.createGenesisConfigMap(ctx, config); err != nil {
		return fmt.Errorf("failed to create genesis configmap: %w", err)
	}

	// Create headless service for internal communication
	if err := e.createHeadlessService(ctx); err != nil {
		return fmt.Errorf("failed to create headless service: %w", err)
	}

	// Create LoadBalancer service for external access
	if err := e.createLoadBalancerService(ctx); err != nil {
		return fmt.Errorf("failed to create loadbalancer service: %w", err)
	}

	// Create StatefulSet
	if err := e.createStatefulSet(ctx, config); err != nil {
		return fmt.Errorf("failed to create statefulset: %w", err)
	}

	e.running = true
	e.startTime = time.Now()

	return nil
}

// Stop removes the Kubernetes resources
func (e *Engine) Stop(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	// Delete StatefulSet (will also delete pods)
	err := e.client.AppsV1().StatefulSets(e.namespace).Delete(ctx, "luxd", metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete statefulset: %w", err)
	}

	// Note: We don't delete the namespace or PVCs to preserve data
	// Use Clean() for full cleanup

	e.running = false
	return nil
}

// Clean removes all K8s resources including PVCs and namespace
func (e *Engine) Clean(ctx context.Context) error {
	if err := e.Stop(ctx); err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// Delete PVCs
	pvcs, _ := e.client.CoreV1().PersistentVolumeClaims(e.namespace).List(ctx, metav1.ListOptions{})
	for _, pvc := range pvcs.Items {
		_ = e.client.CoreV1().PersistentVolumeClaims(e.namespace).Delete(ctx, pvc.Name, metav1.DeleteOptions{})
	}

	// Delete namespace
	return e.client.CoreV1().Namespaces().Delete(ctx, e.namespace, metav1.DeleteOptions{})
}

// Restart performs a rolling restart
func (e *Engine) Restart(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return fmt.Errorf("engine not running")
	}

	// Trigger rolling restart by updating an annotation
	sts, err := e.client.AppsV1().StatefulSets(e.namespace).Get(ctx, "luxd", metav1.GetOptions{})
	if err != nil {
		return err
	}

	if sts.Spec.Template.Annotations == nil {
		sts.Spec.Template.Annotations = make(map[string]string)
	}
	sts.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] = time.Now().Format(time.RFC3339)

	_, err = e.client.AppsV1().StatefulSets(e.namespace).Update(ctx, sts, metav1.UpdateOptions{})
	return err
}

// Health checks the health of the deployed network
func (e *Engine) Health(ctx context.Context) (*engines.HealthStatus, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if !e.running {
		return &engines.HealthStatus{Healthy: false}, nil
	}

	sts, err := e.client.AppsV1().StatefulSets(e.namespace).Get(ctx, "luxd", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	return &engines.HealthStatus{
		Healthy:   sts.Status.ReadyReplicas == *sts.Spec.Replicas,
		PeerCount: int(sts.Status.ReadyReplicas),
		Version:   e.image,
	}, nil
}

// IsRunning returns whether the engine is running
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// Uptime returns how long the engine has been running
func (e *Engine) Uptime() time.Duration {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if !e.running {
		return 0
	}
	return time.Since(e.startTime)
}

// NetworkID returns the network ID
func (e *Engine) NetworkID() uint32 {
	return e.networkID
}

// ChainID returns the chain ID
func (e *Engine) ChainID() ids.ID {
	return e.chainID
}

// RPCEndpoint returns the external RPC endpoint
func (e *Engine) RPCEndpoint() string {
	return fmt.Sprintf("http://luxd.%s.svc.cluster.local:%d/ext/bc/C/rpc", e.namespace, e.httpPort)
}

// WSEndpoint returns the external WebSocket endpoint
func (e *Engine) WSEndpoint() string {
	return fmt.Sprintf("ws://luxd.%s.svc.cluster.local:%d/ext/bc/C/ws", e.namespace, e.httpPort)
}

// P2PEndpoint returns the staking/P2P endpoint
func (e *Engine) P2PEndpoint() string {
	return fmt.Sprintf("luxd.%s.svc.cluster.local:%d", e.namespace, e.stakingPort)
}

// ParentChain returns nil (Lux is L1)
func (e *Engine) ParentChain() *engines.ChainInfo {
	return nil
}

// Metrics returns engine metrics
func (e *Engine) Metrics() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"running":   e.running,
		"replicas":  e.replicas,
		"namespace": e.namespace,
		"image":     e.image,
		"uptime_s":  e.Uptime().Seconds(),
	}
}

// GetExternalIP returns the external LoadBalancer IP
func (e *Engine) GetExternalIP(ctx context.Context) (string, error) {
	svc, err := e.client.CoreV1().Services(e.namespace).Get(ctx, "luxd", metav1.GetOptions{})
	if err != nil {
		return "", err
	}

	for _, ingress := range svc.Status.LoadBalancer.Ingress {
		if ingress.IP != "" {
			return ingress.IP, nil
		}
	}

	return "", fmt.Errorf("no external IP assigned yet")
}

// createNamespace creates the namespace for this network
func (e *Engine) createNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: e.namespace,
			Labels: map[string]string{
				labelNetwork: e.name,
			},
		},
	}

	_, err := e.client.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// createGenesisConfigMap creates ConfigMap with genesis configuration
func (e *Engine) createGenesisConfigMap(ctx context.Context, config *engines.NodeConfig) error {
	// Genesis JSON from config.Extra or use embedded genesis
	genesisJSON := "{}" // Default empty, should be passed via config.Extra["genesis"]
	if g, ok := config.Extra["genesis"].(string); ok {
		genesisJSON = g
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "luxd-genesis",
			Namespace: e.namespace,
		},
		Data: map[string]string{
			"genesis.json": genesisJSON,
		},
	}

	_, err := e.client.CoreV1().ConfigMaps(e.namespace).Create(ctx, cm, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		_, err = e.client.CoreV1().ConfigMaps(e.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	}
	return err
}

// createHeadlessService creates the headless service for StatefulSet DNS
func (e *Engine) createHeadlessService(ctx context.Context) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "luxd-headless",
			Namespace: e.namespace,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Selector: map[string]string{
				labelApp: "luxd",
			},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: int32(e.httpPort), TargetPort: intstr.FromInt(int(e.httpPort))},
				{Name: "staking", Port: int32(e.stakingPort), TargetPort: intstr.FromInt(int(e.stakingPort))},
			},
		},
	}

	_, err := e.client.CoreV1().Services(e.namespace).Create(ctx, svc, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// createLoadBalancerService creates the external LoadBalancer service
func (e *Engine) createLoadBalancerService(ctx context.Context) error {
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "luxd",
			Namespace: e.namespace,
			Annotations: map[string]string{
				"service.beta.kubernetes.io/do-loadbalancer-name": fmt.Sprintf("luxd-%s", e.name),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeLoadBalancer,
			Selector: map[string]string{
				labelApp: "luxd",
			},
			Ports: []corev1.ServicePort{
				{Name: "http", Port: int32(e.httpPort), TargetPort: intstr.FromInt(int(e.httpPort))},
				{Name: "staking", Port: int32(e.stakingPort), TargetPort: intstr.FromInt(int(e.stakingPort))},
			},
		},
	}

	_, err := e.client.CoreV1().Services(e.namespace).Create(ctx, svc, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

// createStatefulSet creates the StatefulSet for validator nodes
func (e *Engine) createStatefulSet(ctx context.Context, config *engines.NodeConfig) error {
	// Build bootstrap IPs from headless service DNS
	var bootstrapIPs []string
	for i := int32(0); i < e.replicas; i++ {
		bootstrapIPs = append(bootstrapIPs, fmt.Sprintf("luxd-%d.luxd-headless.%s.svc.cluster.local:%d", i, e.namespace, e.stakingPort))
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "luxd",
			Namespace: e.namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "luxd-headless",
			Replicas:    &e.replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{labelApp: "luxd"},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						labelApp:     "luxd",
						labelNetwork: e.name,
					},
				},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup:    int64Ptr(1000),
						RunAsUser:  int64Ptr(1000),
						RunAsGroup: int64Ptr(1000),
					},
					Containers: []corev1.Container{{
						Name:            "luxd",
						Image:           e.image,
						ImagePullPolicy: corev1.PullAlways,
						Ports: []corev1.ContainerPort{
							{Name: "http", ContainerPort: int32(e.httpPort)},
							{Name: "staking", ContainerPort: int32(e.stakingPort)},
						},
						Env: []corev1.EnvVar{
							{Name: "NETWORK_ID", Value: fmt.Sprintf("%d", config.NetworkID)},
							{Name: "HTTP_HOST", Value: "0.0.0.0"},
							{Name: "HTTP_PORT", Value: fmt.Sprintf("%d", e.httpPort)},
							{Name: "STAKING_PORT", Value: fmt.Sprintf("%d", e.stakingPort)},
							{Name: "LOG_LEVEL", Value: config.LogLevel},
							{Name: "BOOTSTRAP_IPS", Value: strings.Join(bootstrapIPs[:1], ",")}, // First node bootstraps others
							{Name: "DB_TYPE", Value: "pebbledb"},
							{Name: "INDEX_ENABLED", Value: "true"},
							{Name: "API_ADMIN_ENABLED", Value: "true"},
						},
						VolumeMounts: []corev1.VolumeMount{
							{Name: "data", MountPath: "/data"},
							{Name: "genesis", MountPath: "/genesis", ReadOnly: true},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse(defaultMemoryRequest),
								corev1.ResourceCPU:    resource.MustParse(defaultCPURequest),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse(defaultMemoryLimit),
								corev1.ResourceCPU:    resource.MustParse(defaultCPULimit),
							},
						},
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ext/health",
									Port: intstr.FromInt(int(e.httpPort)),
								},
							},
							InitialDelaySeconds: 60,
							PeriodSeconds:       30,
						},
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ext/info",
									Port: intstr.FromInt(int(e.httpPort)),
								},
							},
							InitialDelaySeconds: 30,
							PeriodSeconds:       10,
						},
					}},
					Volumes: []corev1.Volume{
						{
							Name: "genesis",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "luxd-genesis"},
								},
							},
						},
					},
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{{
				ObjectMeta: metav1.ObjectMeta{Name: "data"},
				Spec: corev1.PersistentVolumeClaimSpec{
					AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
					StorageClassName: &e.storageClass,
					Resources: corev1.VolumeResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceStorage: resource.MustParse(defaultStorageSize),
						},
					},
				},
			}},
		},
	}

	_, err := e.client.AppsV1().StatefulSets(e.namespace).Create(ctx, sts, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		_, err = e.client.AppsV1().StatefulSets(e.namespace).Update(ctx, sts, metav1.UpdateOptions{})
	}
	return err
}

func int64Ptr(i int64) *int64 {
	return &i
}
