package kube

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"geomind/session/internal/session"
)

// Bounds on container resources. Specs are built by the control plane, but the
// runtime never trusts them enough to hand them to the API server unchecked.
const (
	maxCPUQuantity    = 32
	maxMemoryQuantity = 64 << 30
	tmpVolumeSize     = "256Mi"
	homeVolumeSize    = "1Gi"
	nameLabelKey      = "kubernetes.io/metadata.name"
)

var (
	tmpVolumeSizeLimit  = resource.MustParse(tmpVolumeSize)
	homeVolumeSizeLimit = resource.MustParse(homeVolumeSize)
)

func buildSecret(name, namespace string, spec session.Spec, token []byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   namespace,
			Labels:      labelsFor(name),
			Annotations: sessionAnnotations(spec),
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{TokenSecretKey: token},
	}
}

func buildNetworkPolicy(name string, spec session.Spec, opts Options) *networkingv1.NetworkPolicy {
	tcp := corev1.ProtocolTCP
	vncPort := intstr.FromString("vnc")
	proxyPort := intstr.FromInt32(opts.ProxyPort)

	return &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:        networkPolicyName(name),
			Namespace:   opts.Namespace,
			Labels:      labelsFor(name),
			Annotations: sessionAnnotations(spec),
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{MatchLabels: map[string]string{SessionLabel: name}},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
				networkingv1.PolicyTypeEgress,
			},
			Ingress: []networkingv1.NetworkPolicyIngressRule{{
				From: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: namespaceSelector(opts.EdgeNamespace),
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{ComponentLabel: ComponentEdge},
					},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &vncPort}},
			}},
			Egress: []networkingv1.NetworkPolicyEgressRule{{
				To: []networkingv1.NetworkPolicyPeer{{
					NamespaceSelector: namespaceSelector(opts.ProxyNamespace),
					PodSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{ComponentLabel: spec.Egress},
					},
				}},
				Ports: []networkingv1.NetworkPolicyPort{{Protocol: &tcp, Port: &proxyPort}},
			}},
		},
	}
}

// buildPod renders the session Pod. There is deliberately no service account
// token, no host namespace, no writable root filesystem, and no DNS egress
// rule: a routed tab resolves names at the exit, through the proxy.
func buildPod(name string, spec session.Spec, opts Options) (*corev1.Pod, error) {
	cpu, err := parsePositiveQuantity("cpu", spec.CPU)
	if err != nil {
		return nil, err
	}
	memory, err := parsePositiveQuantity("memory", spec.Memory)
	if err != nil {
		return nil, err
	}
	labels := labelsFor(name)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Namespace:   opts.Namespace,
			Labels:      labels,
			Annotations: sessionAnnotations(spec),
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			AutomountServiceAccountToken:  ptrTo(false),
			EnableServiceLinks:            ptrTo(false),
			TerminationGracePeriodSeconds: ptrTo(int64(0)),
			ActiveDeadlineSeconds:         ptrTo(activeDeadline(spec.TTL)),
			SecurityContext: &corev1.PodSecurityContext{
				RunAsNonRoot: ptrTo(true),
				RunAsUser:    ptrTo(defaultSessionUser),
				RunAsGroup:   ptrTo(defaultSessionUser),
				FSGroup:      ptrTo(defaultSessionUser),
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "tmp",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &tmpVolumeSizeLimit},
					},
				},
				{
					Name: "home",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{SizeLimit: &homeVolumeSizeLimit},
					},
				},
			},
			Containers: []corev1.Container{{
				Name:            "session",
				Image:           opts.Image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Ports: []corev1.ContainerPort{{
					Name:          "vnc",
					ContainerPort: SessionPort,
					Protocol:      corev1.ProtocolTCP,
				}},
				Env: []corev1.EnvVar{
					{Name: "GEOMIND_SESSION_ID", Value: spec.ID},
					{Name: "GEOMIND_SESSION_REGION", Value: spec.Region},
					{
						Name: "GEOMIND_SESSION_TOKEN",
						ValueFrom: &corev1.EnvVarSource{
							SecretKeyRef: &corev1.SecretKeySelector{
								LocalObjectReference: corev1.LocalObjectReference{Name: name},
								Key:                  TokenSecretKey,
							},
						},
					},
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    cpu,
						corev1.ResourceMemory: memory,
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    cpu,
						corev1.ResourceMemory: memory,
					},
				},
				SecurityContext: &corev1.SecurityContext{
					AllowPrivilegeEscalation: ptrTo(false),
					ReadOnlyRootFilesystem:   ptrTo(true),
					Capabilities: &corev1.Capabilities{
						Drop: []corev1.Capability{"ALL"},
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{Name: "tmp", MountPath: "/tmp"},
					{Name: "home", MountPath: "/home/geomind"},
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromString("vnc")},
					},
					InitialDelaySeconds: 2,
					PeriodSeconds:       2,
					TimeoutSeconds:      1,
					FailureThreshold:    30,
				},
			}},
		},
	}, nil
}

func sessionAnnotations(spec session.Spec) map[string]string {
	return map[string]string{
		SessionIDAnnotation: spec.ID,
		RegionAnnotation:    spec.Region,
		TTLAnnotation:       strconv.FormatInt(activeDeadline(spec.TTL), 10),
		IdleAnnotation:      strconv.FormatInt(int64(math.Ceil(spec.Idle.Seconds())), 10),
	}
}

func namespaceSelector(namespace string) *metav1.LabelSelector {
	return &metav1.LabelSelector{MatchLabels: map[string]string{nameLabelKey: namespace}}
}

func parsePositiveQuantity(field, value string) (resource.Quantity, error) {
	if strings.TrimSpace(value) == "" {
		return resource.Quantity{}, fmt.Errorf("kube: %s is required", field)
	}
	q, err := resource.ParseQuantity(value)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("kube: %s %q is invalid: %w", field, value, err)
	}
	if q.Sign() <= 0 {
		return resource.Quantity{}, fmt.Errorf("kube: %s %q must be positive", field, value)
	}
	limit := int64(maxCPUQuantity)
	if field == "memory" {
		limit = int64(maxMemoryQuantity)
	}
	if q.Value() > limit {
		return resource.Quantity{}, fmt.Errorf("kube: %s %q exceeds the maximum of %d", field, value, limit)
	}
	return q, nil
}

func ptrTo[T any](value T) *T {
	return &value
}
