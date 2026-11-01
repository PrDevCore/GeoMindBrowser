// Package kube provisions one ephemeral browser Pod per remote session. It
// creates a Pod, a per-session secret, and a default-deny NetworkPolicy, waits
// for readiness, and tears everything down in an order that never removes the
// egress policy while the Pod can still run.
package kube

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"geomind/session/internal/session"
)

// Manifest labels and annotations. The full session ID is an annotation; the
// label carries the derived, DNS-safe workload name.
const (
	ComponentLabel      = "geomind.dev/component"
	ComponentSession    = "session"
	ComponentProxy      = "session-proxy"
	ComponentEdge       = "streaming-edge"
	SessionLabel        = "geomind.dev/session"
	SessionIDAnnotation = "geomind.dev/session-id"
	RegionAnnotation    = "geomind.dev/region"
	TTLAnnotation       = "geomind.dev/ttl-seconds"
	IdleAnnotation      = "geomind.dev/idle-seconds"
	AppNameLabel        = "app.kubernetes.io/name"
	AppNameValue        = "geomind-session"
	ManagedByLabel      = "app.kubernetes.io/managed-by"
	ManagedByValue      = "geomind-sessiond"
	TokenSecretKey      = "session-token"
	// SessionPort is the KasmVNC listener the streaming edge connects to.
	SessionPort int32 = 6901
)

const (
	defaultProxyPort    int32 = 1080
	defaultReadyTimeout       = 2 * time.Minute
	defaultPollInterval       = time.Second
	minPollInterval           = 10 * time.Millisecond
	maxReadyTimeout           = 30 * time.Minute
	defaultSessionUser  int64 = 10001
)

// Options configure the Kubernetes runtime.
type Options struct {
	Namespace      string
	Image          string
	ProxyNamespace string
	ProxyPort      int32
	EdgeNamespace  string
	ReadyTimeout   time.Duration
	PollInterval   time.Duration
}

// Runtime implements session.Runtime on a Kubernetes cluster.
type Runtime struct {
	client kubernetes.Interface
	opts   Options
}

// New builds a runtime from a rest.Config.
func New(cfg *rest.Config, opts Options) (*Runtime, error) {
	if cfg == nil {
		return nil, errors.New("kube: rest config is required")
	}
	client, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kube: client: %w", err)
	}
	return NewForClient(client, opts)
}

// NewForClient builds a runtime from a client. Tests pass a fake clientset.
func NewForClient(client kubernetes.Interface, opts Options) (*Runtime, error) {
	if client == nil {
		return nil, errors.New("kube: client is required")
	}
	if err := opts.applyDefaults(); err != nil {
		return nil, err
	}
	return &Runtime{client: client, opts: opts}, nil
}

func (o *Options) applyDefaults() error {
	if err := requireLabel("namespace", o.Namespace); err != nil {
		return err
	}
	if o.ProxyNamespace == "" {
		o.ProxyNamespace = o.Namespace
	}
	if o.EdgeNamespace == "" {
		o.EdgeNamespace = o.Namespace
	}
	if err := requireLabel("proxy namespace", o.ProxyNamespace); err != nil {
		return err
	}
	if err := requireLabel("edge namespace", o.EdgeNamespace); err != nil {
		return err
	}
	if strings.TrimSpace(o.Image) == "" {
		return errors.New("kube: image is required")
	}
	if err := requireText("image", o.Image, 512); err != nil {
		return err
	}
	if strings.ContainsAny(o.Image, " \t") {
		return errors.New("kube: image must not contain whitespace")
	}
	if o.ProxyPort == 0 {
		o.ProxyPort = defaultProxyPort
	}
	if o.ProxyPort < 1 || o.ProxyPort > 65535 {
		return fmt.Errorf("kube: proxy port %d is out of range", o.ProxyPort)
	}
	if o.ReadyTimeout == 0 {
		o.ReadyTimeout = defaultReadyTimeout
	}
	if o.ReadyTimeout < time.Second || o.ReadyTimeout > maxReadyTimeout {
		return fmt.Errorf("kube: ready timeout %s is out of range", o.ReadyTimeout)
	}
	if o.PollInterval == 0 {
		o.PollInterval = defaultPollInterval
	}
	if o.PollInterval < minPollInterval || o.PollInterval > 30*time.Second {
		return fmt.Errorf("kube: poll interval %s is out of range", o.PollInterval)
	}
	return nil
}

// Create provisions the session workload and waits for the Pod to be ready.
func (r *Runtime) Create(ctx context.Context, spec session.Spec) (session.RuntimeSession, error) {
	if err := validateSpec(spec); err != nil {
		return session.RuntimeSession{}, err
	}
	name := workloadName(spec.ID)
	token, err := randomToken()
	if err != nil {
		return session.RuntimeSession{}, fmt.Errorf("kube: session token: %w", err)
	}

	secret := buildSecret(name, r.opts.Namespace, spec, token)
	if err := createIgnoringExists(func() error {
		_, err := r.client.CoreV1().Secrets(r.opts.Namespace).Create(ctx, secret, metav1.CreateOptions{})
		return err
	}); err != nil {
		return session.RuntimeSession{}, fmt.Errorf("kube: create secret: %w", err)
	}

	policy := buildNetworkPolicy(name, spec, r.opts)
	if err := createIgnoringExists(func() error {
		_, err := r.client.NetworkingV1().NetworkPolicies(r.opts.Namespace).Create(ctx, policy, metav1.CreateOptions{})
		return err
	}); err != nil {
		return session.RuntimeSession{}, fmt.Errorf("kube: create network policy: %w", err)
	}

	pod, err := buildPod(name, spec, r.opts)
	if err != nil {
		return session.RuntimeSession{}, err
	}
	if err := createIgnoringExists(func() error {
		_, err := r.client.CoreV1().Pods(r.opts.Namespace).Create(ctx, pod, metav1.CreateOptions{})
		return err
	}); err != nil {
		return session.RuntimeSession{}, fmt.Errorf("kube: create pod: %w", err)
	}

	return r.waitReady(ctx, spec.ID)
}

// Get maps the Pod state onto the control-plane state machine.
func (r *Runtime) Get(ctx context.Context, id string) (session.RuntimeSession, error) {
	pod, err := r.client.CoreV1().Pods(r.opts.Namespace).Get(ctx, workloadName(id), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return session.RuntimeSession{}, session.ErrRuntimeNotFound
	}
	if err != nil {
		return session.RuntimeSession{}, fmt.Errorf("kube: get pod: %w", err)
	}
	return session.RuntimeSession{ID: id, State: podState(pod)}, nil
}

// Delete removes the Pod first and only removes the NetworkPolicy and secret
// after the Pod is gone, so egress is never unrestricted for a running Pod.
// Every step is idempotent.
func (r *Runtime) Delete(ctx context.Context, id string) error {
	name := workloadName(id)
	podClient := r.client.CoreV1().Pods(r.opts.Namespace)

	err := podClient.Delete(ctx, name, deleteOptions())
	switch {
	case apierrors.IsNotFound(err):
	case err != nil:
		return fmt.Errorf("kube: delete pod: %w", err)
	default:
		if err := r.waitPodGone(ctx, name); err != nil {
			return err
		}
	}

	if err := r.client.NetworkingV1().NetworkPolicies(r.opts.Namespace).Delete(ctx, networkPolicyName(name), deleteOptions()); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("kube: delete network policy: %w", err)
	}
	if err := r.client.CoreV1().Secrets(r.opts.Namespace).Delete(ctx, name, deleteOptions()); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("kube: delete secret: %w", err)
	}
	return nil
}

func (r *Runtime) waitReady(ctx context.Context, id string) (session.RuntimeSession, error) {
	waitCtx, cancel := context.WithTimeout(ctx, r.opts.ReadyTimeout)
	defer cancel()

	ticker := time.NewTicker(r.opts.PollInterval)
	defer ticker.Stop()
	for {
		res, err := r.Get(waitCtx, id)
		if err != nil {
			if errors.Is(err, session.ErrRuntimeNotFound) {
				return session.RuntimeSession{}, errors.New("kube: session pod disappeared during provisioning")
			}
			return session.RuntimeSession{}, err
		}
		switch res.State {
		case session.StateReady, session.StateActive:
			return res, nil
		case session.StateFailed:
			return session.RuntimeSession{}, errors.New("kube: session pod failed")
		}
		select {
		case <-waitCtx.Done():
			return session.RuntimeSession{}, fmt.Errorf("kube: session was not ready within %s", r.opts.ReadyTimeout)
		case <-ticker.C:
		}
	}
}

func (r *Runtime) waitPodGone(ctx context.Context, name string) error {
	ticker := time.NewTicker(r.opts.PollInterval)
	defer ticker.Stop()
	for {
		_, err := r.client.CoreV1().Pods(r.opts.Namespace).Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("kube: wait for pod deletion: %w", err)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("kube: pod %s is still terminating", name)
		case <-ticker.C:
		}
	}
}

func podState(pod *corev1.Pod) session.State {
	if pod.DeletionTimestamp != nil {
		return session.StateTerminating
	}
	switch pod.Status.Phase {
	case corev1.PodRunning:
		if podReady(pod) {
			return session.StateReady
		}
		return session.StateProvisioning
	case corev1.PodFailed:
		return session.StateFailed
	case corev1.PodSucceeded:
		return session.StateGone
	default:
		return session.StateProvisioning
	}
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func validateSpec(spec session.Spec) error {
	if err := requireText("session id", spec.ID, 64); err != nil {
		return err
	}
	if err := requireText("region", spec.Region, 64); err != nil {
		return err
	}
	if spec.TTL <= 0 {
		return errors.New("kube: ttl must be positive")
	}
	if spec.Idle < 0 || spec.Idle > spec.TTL {
		return errors.New("kube: idle must be between zero and ttl")
	}
	if _, err := parsePositiveQuantity("cpu", spec.CPU); err != nil {
		return err
	}
	if _, err := parsePositiveQuantity("memory", spec.Memory); err != nil {
		return err
	}
	if err := requireLabel("egress component", spec.Egress); err != nil {
		return err
	}
	return nil
}

func workloadName(sessionID string) string {
	sum := sha256.Sum256([]byte(sessionID))
	return "gms-" + hex.EncodeToString(sum[:])[:40]
}

func networkPolicyName(name string) string {
	return name + "-net"
}

func labelsFor(name string) map[string]string {
	return map[string]string{
		AppNameLabel:   AppNameValue,
		ManagedByLabel: ManagedByValue,
		ComponentLabel: ComponentSession,
		SessionLabel:   name,
	}
}

func randomToken() ([]byte, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func createIgnoringExists(create func() error) error {
	if err := create(); err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func deleteOptions() metav1.DeleteOptions {
	grace := int64(0)
	policy := metav1.DeletePropagationBackground
	return metav1.DeleteOptions{GracePeriodSeconds: &grace, PropagationPolicy: &policy}
}

func activeDeadline(ttl time.Duration) int64 {
	seconds := int64(math.Ceil(ttl.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func requireLabel(field, value string) error {
	if problems := validation.IsDNS1123Label(value); len(problems) > 0 {
		return fmt.Errorf("kube: %s %q is not a valid DNS label: %s", field, value, strings.Join(problems, "; "))
	}
	return nil
}

func requireText(field, value string, maxLen int) error {
	if value == "" {
		return fmt.Errorf("kube: %s is required", field)
	}
	if len(value) > maxLen {
		return fmt.Errorf("kube: %s is longer than %d bytes", field, maxLen)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return fmt.Errorf("kube: %s contains non-printable characters", field)
		}
	}
	return nil
}
