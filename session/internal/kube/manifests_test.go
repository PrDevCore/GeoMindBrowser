package kube

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"geomind/session/internal/session"
)

var workloadNamePattern = regexp.MustCompile(`^gms-[0-9a-f]{40}$`)

func baseSpec() session.Spec {
	return session.Spec{
		ID:     "ses_0123456789abcdef",
		Region: "de-test",
		TTL:    time.Hour,
		Idle:   5 * time.Minute,
		CPU:    "500m",
		Memory: "512Mi",
		Egress: ComponentProxy,
	}
}

func baseOptions() Options {
	return Options{
		Namespace:      "sessions",
		Image:          "registry.example/geomind-session:test",
		ProxyNamespace: "egress",
		EdgeNamespace:  "edge",
		ProxyPort:      1080,
	}
}

func TestWorkloadNameIsDerivedAndDNS1123(t *testing.T) {
	name := workloadName(baseSpec().ID)
	if !workloadNamePattern.MatchString(name) {
		t.Fatalf("workloadName = %q, want gms- plus 40 hex characters", name)
	}
	if len(name) > 63 {
		t.Fatalf("workloadName length %d exceeds a DNS label", len(name))
	}
	if strings.Contains(name, baseSpec().ID) {
		t.Fatalf("workloadName leaks the session id: %q", name)
	}
	if got := workloadName(baseSpec().ID); got != name {
		t.Fatalf("workloadName is not deterministic: %q then %q", name, got)
	}
	if other := workloadName("ses_fedcba9876543210"); other == name {
		t.Fatal("different session ids produced the same workload name")
	}
}

func TestBuildSecretCarriesOnlyTheToken(t *testing.T) {
	spec := baseSpec()
	token := bytes.Repeat([]byte{0xab}, 32)
	secret := buildSecret(workloadName(spec.ID), "sessions", spec, token)

	if secret.Namespace != "sessions" {
		t.Errorf("namespace = %q", secret.Namespace)
	}
	if secret.Type != corev1.SecretTypeOpaque {
		t.Errorf("type = %q", secret.Type)
	}
	if len(secret.Data) != 1 {
		t.Fatalf("data keys = %d, want only the token", len(secret.Data))
	}
	if !bytes.Equal(secret.Data[TokenSecretKey], token) {
		t.Error("token bytes were not stored under the session token key")
	}
	if len(secret.StringData) != 0 {
		t.Error("secret must not carry string data")
	}
	if len(secret.OwnerReferences) != 0 {
		t.Error("secret must not take an owner reference")
	}
	if got := secret.Annotations[SessionIDAnnotation]; got != spec.ID {
		t.Errorf("session annotation = %q, want %q", got, spec.ID)
	}
	if got := secret.Annotations[RegionAnnotation]; got != spec.Region {
		t.Errorf("region annotation = %q", got)
	}
}

func TestBuildNetworkPolicyDeniesByDefault(t *testing.T) {
	spec := baseSpec()
	opts := baseOptions()
	name := workloadName(spec.ID)
	policy := buildNetworkPolicy(name, spec, opts)

	if policy.Namespace != opts.Namespace {
		t.Errorf("namespace = %q", policy.Namespace)
	}
	if got := policy.Spec.PodSelector.MatchLabels[SessionLabel]; got != name {
		t.Errorf("pod selector = %q, want %q", got, name)
	}
	if !containsPolicyType(policy.Spec.PolicyTypes, networkingv1.PolicyTypeIngress) ||
		!containsPolicyType(policy.Spec.PolicyTypes, networkingv1.PolicyTypeEgress) {
		t.Fatalf("policy types = %v, want ingress and egress", policy.Spec.PolicyTypes)
	}
	if len(policy.Spec.Ingress) != 1 {
		t.Fatalf("ingress rules = %d, want exactly one", len(policy.Spec.Ingress))
	}
	if len(policy.Spec.Egress) != 1 {
		t.Fatalf("egress rules = %d, want exactly one", len(policy.Spec.Egress))
	}

	ingress := policy.Spec.Ingress[0]
	if len(ingress.From) != 1 {
		t.Fatalf("ingress peers = %d", len(ingress.From))
	}
	if got := ingress.From[0].NamespaceSelector.MatchLabels[nameLabelKey]; got != opts.EdgeNamespace {
		t.Errorf("ingress namespace selector = %q", got)
	}
	if got := ingress.From[0].PodSelector.MatchLabels[ComponentLabel]; got != ComponentEdge {
		t.Errorf("ingress component selector = %q", got)
	}
	if len(ingress.Ports) != 1 || ingress.Ports[0].Port == nil || ingress.Ports[0].Port.String() != "vnc" {
		t.Errorf("ingress ports = %v, want only the named vnc port", ingress.Ports)
	}
	if got := *ingress.Ports[0].Protocol; got != corev1.ProtocolTCP {
		t.Errorf("ingress protocol = %q", got)
	}

	egress := policy.Spec.Egress[0]
	if len(egress.To) != 1 {
		t.Fatalf("egress peers = %d", len(egress.To))
	}
	if got := egress.To[0].NamespaceSelector.MatchLabels[nameLabelKey]; got != opts.ProxyNamespace {
		t.Errorf("egress namespace selector = %q", got)
	}
	if got := egress.To[0].PodSelector.MatchLabels[ComponentLabel]; got != spec.Egress {
		t.Errorf("egress component selector = %q", got)
	}
	if len(egress.Ports) != 1 || egress.Ports[0].Port == nil || egress.Ports[0].Port.IntValue() != int(opts.ProxyPort) {
		t.Errorf("egress ports = %v, want TCP %d", egress.Ports, opts.ProxyPort)
	}
	if got := *egress.Ports[0].Protocol; got != corev1.ProtocolTCP {
		t.Errorf("egress protocol = %q", got)
	}

	for _, rule := range policy.Spec.Egress {
		for _, port := range rule.Ports {
			if port.Port != nil && port.Port.IntValue() == 53 {
				t.Fatal("network policy allows DNS egress; routed names must resolve at the exit")
			}
		}
		for _, peer := range rule.To {
			if peer.PodSelector == nil && peer.NamespaceSelector == nil && peer.IPBlock == nil {
				t.Fatal("network policy has an empty egress peer, which allows all destinations")
			}
		}
	}
	for _, rule := range policy.Spec.Ingress {
		for _, peer := range rule.From {
			if peer.PodSelector == nil && peer.NamespaceSelector == nil && peer.IPBlock == nil {
				t.Fatal("network policy has an empty ingress peer, which allows all sources")
			}
		}
	}
}

func TestBuildPodIsLockedDown(t *testing.T) {
	spec := baseSpec()
	opts := baseOptions()
	name := workloadName(spec.ID)
	pod, err := buildPod(name, spec, opts)
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}

	if pod.Namespace != opts.Namespace {
		t.Errorf("namespace = %q", pod.Namespace)
	}
	if pod.Spec.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restart policy = %q", pod.Spec.RestartPolicy)
	}
	if pod.Spec.ServiceAccountName != "" {
		t.Errorf("service account = %q, want the default with automount disabled", pod.Spec.ServiceAccountName)
	}
	if pod.Spec.AutomountServiceAccountToken == nil || *pod.Spec.AutomountServiceAccountToken {
		t.Error("service account token must not be automounted")
	}
	if pod.Spec.EnableServiceLinks == nil || *pod.Spec.EnableServiceLinks {
		t.Error("service links must be disabled")
	}
	if pod.Spec.HostNetwork || pod.Spec.HostPID || pod.Spec.HostIPC {
		t.Error("pod must not share host namespaces")
	}
	if pod.Spec.TerminationGracePeriodSeconds == nil || *pod.Spec.TerminationGracePeriodSeconds != 0 {
		t.Error("pod must terminate without a grace period")
	}
	if pod.Spec.ActiveDeadlineSeconds == nil || *pod.Spec.ActiveDeadlineSeconds != 3600 {
		t.Errorf("active deadline = %v, want the one hour ttl", pod.Spec.ActiveDeadlineSeconds)
	}

	sc := pod.Spec.SecurityContext
	if sc == nil {
		t.Fatal("pod security context is missing")
	}
	if sc.RunAsNonRoot == nil || !*sc.RunAsNonRoot {
		t.Error("pod must run as non-root")
	}
	if sc.RunAsUser == nil || *sc.RunAsUser != defaultSessionUser {
		t.Errorf("runAsUser = %v", sc.RunAsUser)
	}
	if sc.SeccompProfile == nil || sc.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Error("pod must use the runtime default seccomp profile")
	}

	if len(pod.Spec.Volumes) != 2 {
		t.Fatalf("volumes = %d, want tmp and home", len(pod.Spec.Volumes))
	}
	for _, volume := range pod.Spec.Volumes {
		if volume.EmptyDir == nil {
			t.Errorf("volume %q is not an emptyDir", volume.Name)
			continue
		}
		if volume.EmptyDir.SizeLimit == nil {
			t.Errorf("volume %q has no size limit", volume.Name)
		}
	}

	if len(pod.Spec.Containers) != 1 {
		t.Fatalf("containers = %d", len(pod.Spec.Containers))
	}
	container := pod.Spec.Containers[0]
	if container.Image != opts.Image {
		t.Errorf("image = %q", container.Image)
	}
	if container.ImagePullPolicy != corev1.PullIfNotPresent {
		t.Errorf("image pull policy = %q", container.ImagePullPolicy)
	}
	csc := container.SecurityContext
	if csc == nil {
		t.Fatal("container security context is missing")
	}
	if csc.AllowPrivilegeEscalation == nil || *csc.AllowPrivilegeEscalation {
		t.Error("container must not allow privilege escalation")
	}
	if csc.ReadOnlyRootFilesystem == nil || !*csc.ReadOnlyRootFilesystem {
		t.Error("container root filesystem must be read-only")
	}
	if csc.Capabilities == nil || len(csc.Capabilities.Drop) != 1 || csc.Capabilities.Drop[0] != "ALL" {
		t.Errorf("capabilities = %v, want drop ALL", csc.Capabilities)
	}

	if len(container.Ports) != 1 {
		t.Fatalf("container ports = %d", len(container.Ports))
	}
	if container.Ports[0].Name != "vnc" || container.Ports[0].ContainerPort != SessionPort {
		t.Errorf("container port = %+v", container.Ports[0])
	}
	if container.ReadinessProbe == nil || container.ReadinessProbe.TCPSocket == nil {
		t.Fatal("readiness probe must be a TCP socket probe")
	}
	if got := container.ReadinessProbe.TCPSocket.Port.String(); got != "vnc" {
		t.Errorf("probe port = %q, want the named vnc port", got)
	}

	if got := container.Resources.Requests.Cpu().String(); got != spec.CPU {
		t.Errorf("cpu request = %q, want %q", got, spec.CPU)
	}
	if got := container.Resources.Limits.Memory().String(); got != spec.Memory {
		t.Errorf("memory limit = %q, want %q", got, spec.Memory)
	}
	if container.Resources.Requests.Cpu().Cmp(*container.Resources.Limits.Cpu()) != 0 {
		t.Error("cpu request and limit must match")
	}

	var tokenEnv *corev1.EnvVar
	for i := range container.Env {
		if container.Env[i].Name == "GEOMIND_SESSION_TOKEN" {
			tokenEnv = &container.Env[i]
		}
	}
	if tokenEnv == nil || tokenEnv.ValueFrom == nil || tokenEnv.ValueFrom.SecretKeyRef == nil {
		t.Fatal("session token must come from the per-session secret")
	}
	if tokenEnv.ValueFrom.SecretKeyRef.Name != name {
		t.Errorf("secret ref = %q, want %q", tokenEnv.ValueFrom.SecretKeyRef.Name, name)
	}
	if tokenEnv.ValueFrom.SecretKeyRef.Key != TokenSecretKey {
		t.Errorf("secret key = %q", tokenEnv.ValueFrom.SecretKeyRef.Key)
	}
	if tokenEnv.Value != "" {
		t.Error("token must never be inlined into the pod spec")
	}

	if got := pod.Labels[SessionLabel]; got != name {
		t.Errorf("session label = %q, want %q", got, name)
	}
	if got := pod.Labels[ComponentLabel]; got != ComponentSession {
		t.Errorf("component label = %q", got)
	}
	if got := pod.Annotations[SessionIDAnnotation]; got != spec.ID {
		t.Errorf("session annotation = %q", got)
	}
	if got := pod.Annotations[TTLAnnotation]; got != "3600" {
		t.Errorf("ttl annotation = %q", got)
	}
	if got := pod.Annotations[IdleAnnotation]; got != "300" {
		t.Errorf("idle annotation = %q", got)
	}
}

func TestBuildPodRoundsActiveDeadlineUp(t *testing.T) {
	spec := baseSpec()
	spec.TTL = 1500 * time.Millisecond
	pod, err := buildPod(workloadName(spec.ID), spec, baseOptions())
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	if pod.Spec.ActiveDeadlineSeconds == nil || *pod.Spec.ActiveDeadlineSeconds != 2 {
		t.Errorf("active deadline = %v, want 2 seconds", pod.Spec.ActiveDeadlineSeconds)
	}
}

func TestBuildPodRejectsResourceAbuse(t *testing.T) {
	cases := map[string]struct {
		cpu, memory string
	}{
		"huge cpu":       {cpu: "1000", memory: "512Mi"},
		"huge memory":    {cpu: "500m", memory: "1Ti"},
		"zero cpu":       {cpu: "0", memory: "512Mi"},
		"negative limit": {cpu: "-500m", memory: "512Mi"},
		"garbage":        {cpu: "cpu", memory: "512Mi"},
		"empty":          {cpu: "500m", memory: " "},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			spec := baseSpec()
			spec.CPU = tc.cpu
			spec.Memory = tc.memory
			if _, err := buildPod(workloadName(spec.ID), spec, baseOptions()); err == nil {
				t.Fatalf("buildPod accepted cpu=%q memory=%q", tc.cpu, tc.memory)
			}
		})
	}
}

func TestParsePositiveQuantityBounds(t *testing.T) {
	if _, err := parsePositiveQuantity("cpu", "32000m"); err != nil {
		t.Errorf("32 cores were rejected: %v", err)
	}
	if _, err := parsePositiveQuantity("memory", "64Gi"); err != nil {
		t.Errorf("64Gi was rejected: %v", err)
	}
	if _, err := parsePositiveQuantity("cpu", "32001m"); err == nil {
		t.Error("cpu above the bound was accepted")
	}
	if _, err := parsePositiveQuantity("memory", "65Gi"); err == nil {
		t.Error("memory above the bound was accepted")
	}
}

func TestOptionsDefaultsAndValidation(t *testing.T) {
	opts := Options{Namespace: "sessions", Image: "img:tag"}
	if err := opts.applyDefaults(); err != nil {
		t.Fatalf("applyDefaults: %v", err)
	}
	if opts.ProxyNamespace != "sessions" || opts.EdgeNamespace != "sessions" {
		t.Errorf("namespaces did not default: %+v", opts)
	}
	if opts.ProxyPort != defaultProxyPort {
		t.Errorf("proxy port = %d", opts.ProxyPort)
	}
	if opts.ReadyTimeout != defaultReadyTimeout {
		t.Errorf("ready timeout = %s", opts.ReadyTimeout)
	}
	if opts.PollInterval != defaultPollInterval {
		t.Errorf("poll interval = %s", opts.PollInterval)
	}

	cases := map[string]Options{
		"missing namespace": {Image: "img:tag"},
		"bad namespace":     {Namespace: "Sessions", Image: "img:tag"},
		"missing image":     {Namespace: "sessions"},
		"blank image":       {Namespace: "sessions", Image: "  "},
		"image with spaces": {Namespace: "sessions", Image: "img tag"},
		"bad proxy ns":      {Namespace: "sessions", Image: "img:tag", ProxyNamespace: "a/b"},
		"bad edge ns":       {Namespace: "sessions", Image: "img:tag", EdgeNamespace: "a/b"},
		"port zero default": {Namespace: "sessions", Image: "img:tag", ProxyPort: -1},
		"port too high":     {Namespace: "sessions", Image: "img:tag", ProxyPort: 70000},
		"ready too short":   {Namespace: "sessions", Image: "img:tag", ReadyTimeout: time.Millisecond},
		"ready too long":    {Namespace: "sessions", Image: "img:tag", ReadyTimeout: time.Hour},
		"poll too short":    {Namespace: "sessions", Image: "img:tag", PollInterval: time.Millisecond},
		"poll too long":     {Namespace: "sessions", Image: "img:tag", PollInterval: time.Minute},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := tc.applyDefaults(); err == nil {
				t.Fatalf("options were accepted: %+v", tc)
			}
		})
	}
}

func TestNewRequiresClientAndConfig(t *testing.T) {
	if _, err := NewForClient(nil, baseOptions()); err == nil {
		t.Fatal("nil client was accepted")
	}
	if _, err := New(nil, baseOptions()); err == nil {
		t.Fatal("nil rest config was accepted")
	}
}

func TestValidateSpec(t *testing.T) {
	if err := validateSpec(baseSpec()); err != nil {
		t.Fatalf("valid spec rejected: %v", err)
	}
	cases := map[string]func(*session.Spec){
		"empty id":      func(s *session.Spec) { s.ID = "" },
		"long id":       func(s *session.Spec) { s.ID = strings.Repeat("a", 65) },
		"empty region":  func(s *session.Spec) { s.Region = "" },
		"zero ttl":      func(s *session.Spec) { s.TTL = 0 },
		"idle over ttl": func(s *session.Spec) { s.Idle = s.TTL + time.Second },
		"negative idle": func(s *session.Spec) { s.Idle = -time.Second },
		"bad egress":    func(s *session.Spec) { s.Egress = "proxy/one" },
		"empty egress":  func(s *session.Spec) { s.Egress = "" },
		"non printable": func(s *session.Spec) { s.Region = "de\x00" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			spec := baseSpec()
			mutate(&spec)
			if err := validateSpec(spec); err == nil {
				t.Fatalf("spec was accepted: %+v", spec)
			}
		})
	}
}

func TestPodStateMapping(t *testing.T) {
	ready := func(pod *corev1.Pod) *corev1.Pod {
		pod.Status.Phase = corev1.PodRunning
		pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
		return pod
	}
	cases := []struct {
		name string
		pod  *corev1.Pod
		want session.State
	}{
		{name: "pending", pod: &corev1.Pod{}, want: session.StateProvisioning},
		{name: "running not ready", pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodRunning}}, want: session.StateProvisioning},
		{name: "ready", pod: ready(&corev1.Pod{}), want: session.StateReady},
		{name: "failed", pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodFailed}}, want: session.StateFailed},
		{name: "succeeded", pod: &corev1.Pod{Status: corev1.PodStatus{Phase: corev1.PodSucceeded}}, want: session.StateGone},
		{
			name: "deleting",
			pod: &corev1.Pod{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &metav1.Time{Time: time.Now()}},
				Status: corev1.PodStatus{Phase: corev1.PodRunning}},
			want: session.StateTerminating,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := podState(tc.pod); got != tc.want {
				t.Errorf("podState = %q, want %q", got, tc.want)
			}
		})
	}
}

func containsPolicyType(types []networkingv1.PolicyType, want networkingv1.PolicyType) bool {
	for _, got := range types {
		if got == want {
			return true
		}
	}
	return false
}
