package kube

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"

	"geomind/session/internal/session"
)

var _ session.Runtime = (*Runtime)(nil)

func testOptions() Options {
	return Options{
		Namespace:      "sessions",
		Image:          "registry.example/geomind-session:test",
		ProxyNamespace: "egress",
		EdgeNamespace:  "edge",
		ProxyPort:      1080,
		ReadyTimeout:   2 * time.Second,
		PollInterval:   10 * time.Millisecond,
	}
}

func newTestRuntime(t *testing.T) (*Runtime, *fake.Clientset) {
	t.Helper()
	client := fake.NewClientset()
	rt, err := NewForClient(client, testOptions())
	if err != nil {
		t.Fatalf("NewForClient: %v", err)
	}
	return rt, client
}

// drivePod waits for the Pod to exist and then applies mutate once. It stops
// when the test ends so a failing Create cannot leak a goroutine.
func drivePod(t *testing.T, client *fake.Clientset, id string, mutate func(*corev1.Pod)) {
	t.Helper()
	name := workloadName(id)
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
			}
			pod, err := client.CoreV1().Pods("sessions").Get(context.Background(), name, metav1.GetOptions{})
			if err != nil {
				continue
			}
			mutate(pod)
			if _, err := client.CoreV1().Pods("sessions").UpdateStatus(context.Background(), pod, metav1.UpdateOptions{}); err == nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})
}

func markRunning(pod *corev1.Pod) {
	pod.Status.Phase = corev1.PodRunning
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}
}

func TestCreateProvisionsAndWaitsForReadiness(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePod(t, client, spec.ID, markRunning)

	res, err := rt.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if res.ID != spec.ID {
		t.Errorf("runtime echoed id %q, want %q", res.ID, spec.ID)
	}
	if res.State != session.StateReady {
		t.Errorf("state = %q, want ready", res.State)
	}

	name := workloadName(spec.ID)
	secret, err := client.CoreV1().Secrets("sessions").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if len(secret.Data[TokenSecretKey]) != 32 {
		t.Errorf("token length = %d, want 32 random bytes", len(secret.Data[TokenSecretKey]))
	}
	if string(secret.Data[TokenSecretKey]) == string(make([]byte, 32)) {
		t.Error("token is all zeroes")
	}

	policy, err := client.NetworkingV1().NetworkPolicies("sessions").Get(context.Background(), networkPolicyName(name), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("network policy: %v", err)
	}
	if got := policy.Spec.PodSelector.MatchLabels[SessionLabel]; got != name {
		t.Errorf("policy selects %q, want %q", got, name)
	}

	pod, err := client.CoreV1().Pods("sessions").Get(context.Background(), name, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("pod: %v", err)
	}
	if pod.Labels[SessionLabel] != name {
		t.Errorf("pod label = %q", pod.Labels[SessionLabel])
	}
}

func TestCreateIsIdempotentForExistingObjects(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePod(t, client, spec.ID, markRunning)

	if _, err := rt.Create(context.Background(), spec); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if _, err := rt.Create(context.Background(), spec); err != nil {
		t.Fatalf("second Create: %v", err)
	}

	token, err := client.CoreV1().Secrets("sessions").
		Get(context.Background(), workloadName(spec.ID), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("secret: %v", err)
	}
	if len(token.Data[TokenSecretKey]) != 32 {
		t.Error("existing secret was replaced or corrupted")
	}
}

func TestCreateReportsFailedPod(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePod(t, client, spec.ID, func(pod *corev1.Pod) {
		pod.Status.Phase = corev1.PodFailed
	})

	if _, err := rt.Create(context.Background(), spec); err == nil {
		t.Fatal("Create succeeded for a failed pod")
	}
}

func TestCreateReportsPodDisappearance(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePod(t, client, spec.ID, func(pod *corev1.Pod) {
		if err := client.CoreV1().Pods("sessions").Delete(context.Background(), pod.Name, metav1.DeleteOptions{}); err != nil {
			t.Errorf("delete pod: %v", err)
		}
	})

	if _, err := rt.Create(context.Background(), spec); err == nil {
		t.Fatal("Create succeeded after the pod disappeared")
	}
}

func TestCreateTimesOutWithoutReadiness(t *testing.T) {
	opts := testOptions()
	opts.ReadyTimeout = time.Second
	client := fake.NewClientset()
	rt, err := NewForClient(client, opts)
	if err != nil {
		t.Fatalf("NewForClient: %v", err)
	}

	start := time.Now()
	if _, err := rt.Create(context.Background(), baseSpec()); err == nil {
		t.Fatal("Create succeeded without a ready pod")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("Create took %s, ready timeout was %s", elapsed, opts.ReadyTimeout)
	}
}

func TestCreateHonorsCancellation(t *testing.T) {
	rt, _ := newTestRuntime(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := rt.Create(ctx, baseSpec()); err == nil {
		t.Fatal("Create succeeded after the context expired")
	}
}

func TestCreateRejectsInvalidSpecBeforeCallingAPI(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	spec.CPU = "64"

	if _, err := rt.Create(context.Background(), spec); err == nil {
		t.Fatal("Create accepted an oversized cpu request")
	}
	if got := len(client.Actions()); got != 0 {
		t.Errorf("client saw %d actions for an invalid spec", got)
	}
}

func TestCreateStopsWhenSecretCreationFails(t *testing.T) {
	rt, client := newTestRuntime(t)
	client.PrependReactor("create", "secrets", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "x", errors.New("denied"))
	})

	if _, err := rt.Create(context.Background(), baseSpec()); err == nil {
		t.Fatal("Create succeeded after secret creation failed")
	}
	assertCount(t, client, "pods", 0)
	assertCount(t, client, "networkpolicies", 0)
}

func TestGetMapsPodState(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePodOnce(t, client, spec.ID)

	res, err := rt.Get(context.Background(), spec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if res.ID != spec.ID || res.State != session.StateReady {
		t.Errorf("Get = %+v", res)
	}
}

func TestGetReturnsRuntimeNotFound(t *testing.T) {
	rt, _ := newTestRuntime(t)
	if _, err := rt.Get(context.Background(), baseSpec().ID); !errors.Is(err, session.ErrRuntimeNotFound) {
		t.Fatalf("Get error = %v, want ErrRuntimeNotFound", err)
	}
}

func TestDeleteRemovesPodBeforePolicyAndSecret(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePodOnce(t, client, spec.ID)

	var mu sync.Mutex
	var deleted []string
	client.PrependReactor("*", "*", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetVerb() == "delete" {
			mu.Lock()
			deleted = append(deleted, action.GetResource().Resource)
			mu.Unlock()
		}
		return false, nil, nil
	})

	if err := rt.Delete(context.Background(), spec.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	mu.Lock()
	got := append([]string(nil), deleted...)
	mu.Unlock()
	want := []string{"pods", "networkpolicies", "secrets"}
	if len(got) != len(want) {
		t.Fatalf("delete order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delete order = %v, want %v", got, want)
		}
	}

	name := workloadName(spec.ID)
	if _, err := client.CoreV1().Pods("sessions").Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("pod still exists: %v", err)
	}
	if _, err := client.NetworkingV1().NetworkPolicies("sessions").Get(context.Background(), networkPolicyName(name), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("network policy still exists: %v", err)
	}
	if _, err := client.CoreV1().Secrets("sessions").Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("secret still exists: %v", err)
	}
}

func TestDeleteIsIdempotent(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePodOnce(t, client, spec.ID)

	if err := rt.Delete(context.Background(), spec.ID); err != nil {
		t.Fatalf("first Delete: %v", err)
	}
	if err := rt.Delete(context.Background(), spec.ID); err != nil {
		t.Fatalf("second Delete: %v", err)
	}
	if err := rt.Delete(context.Background(), "ses_neverprovisioned"); err != nil {
		t.Fatalf("Delete of an unknown session: %v", err)
	}
}

func TestDeleteKeepsPolicyWhilePodIsTerminating(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	name := workloadName(spec.ID)

	terminating := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:              name,
			Namespace:         "sessions",
			DeletionTimestamp: &metav1.Time{Time: time.Now()},
			Finalizers:        []string{"test"},
		},
	}
	client.PrependReactor("get", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, terminating.DeepCopy(), nil
	})
	client.PrependReactor("delete", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, nil // pretend the finalizer holds the pod
	})
	seedObjects(t, client, spec)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := rt.Delete(ctx, spec.ID)
	if err == nil {
		t.Fatal("Delete reported success while the pod was still terminating")
	}

	if _, err := client.NetworkingV1().NetworkPolicies("sessions").Get(context.Background(), networkPolicyName(name), metav1.GetOptions{}); err != nil {
		t.Errorf("network policy was removed before the pod: %v", err)
	}
	if _, err := client.CoreV1().Secrets("sessions").Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Errorf("secret was removed before the pod: %v", err)
	}
}

func TestDeleteKeepsPolicyWhenPodDeleteFails(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	name := workloadName(spec.ID)
	seedObjects(t, client, spec)
	client.PrependReactor("delete", "pods", func(ktesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewInternalError(errors.New("apiserver unavailable"))
	})

	if err := rt.Delete(context.Background(), spec.ID); err == nil {
		t.Fatal("Delete succeeded while the pod delete failed")
	}
	if _, err := client.NetworkingV1().NetworkPolicies("sessions").Get(context.Background(), networkPolicyName(name), metav1.GetOptions{}); err != nil {
		t.Errorf("network policy was removed although the pod delete failed: %v", err)
	}
	if _, err := client.CoreV1().Secrets("sessions").Get(context.Background(), name, metav1.GetOptions{}); err != nil {
		t.Errorf("secret was removed although the pod delete failed: %v", err)
	}
}

func TestDeleteCleansUpWhenPodIsAlreadyGone(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	name := workloadName(spec.ID)
	if _, err := client.CoreV1().Secrets("sessions").Create(context.Background(),
		buildSecret(name, "sessions", spec, []byte("token")), metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	if _, err := client.NetworkingV1().NetworkPolicies("sessions").Create(context.Background(),
		buildNetworkPolicy(name, spec, testOptions()), metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed policy: %v", err)
	}

	if err := rt.Delete(context.Background(), spec.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := client.NetworkingV1().NetworkPolicies("sessions").Get(context.Background(), networkPolicyName(name), metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("network policy still exists: %v", err)
	}
	if _, err := client.CoreV1().Secrets("sessions").Get(context.Background(), name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Errorf("secret still exists: %v", err)
	}
}

func TestCreateThenDeleteRoundTrip(t *testing.T) {
	rt, client := newTestRuntime(t)
	spec := baseSpec()
	drivePodOnce(t, client, spec.ID)

	if _, err := rt.Create(context.Background(), spec); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := rt.Delete(context.Background(), spec.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := rt.Get(context.Background(), spec.ID); !errors.Is(err, session.ErrRuntimeNotFound) {
		t.Fatalf("Get after Delete = %v, want ErrRuntimeNotFound", err)
	}
}

// drivePodOnce creates a ready Pod directly, for tests that do not exercise
// Create.
func drivePodOnce(t *testing.T, client *fake.Clientset, id string) string {
	t.Helper()
	spec := baseSpec()
	spec.ID = id
	name := workloadName(id)
	pod, err := buildPod(name, spec, testOptions())
	if err != nil {
		t.Fatalf("buildPod: %v", err)
	}
	markRunning(pod)
	if _, err := client.CoreV1().Pods("sessions").Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatalf("create pod: %v", err)
	}
	return name
}

func seedObjects(t *testing.T, client *fake.Clientset, spec session.Spec) {
	t.Helper()
	name := workloadName(spec.ID)
	if _, err := client.CoreV1().Secrets("sessions").Create(context.Background(),
		buildSecret(name, "sessions", spec, []byte("token")), metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed secret: %v", err)
	}
	if _, err := client.NetworkingV1().NetworkPolicies("sessions").Create(context.Background(),
		buildNetworkPolicy(name, spec, testOptions()), metav1.CreateOptions{}); err != nil {
		t.Fatalf("seed policy: %v", err)
	}
}

func assertCount(t *testing.T, client *fake.Clientset, resource string, want int) {
	t.Helper()
	got := 0
	for _, action := range client.Actions() {
		if action.GetVerb() == "create" && action.GetResource().Resource == resource {
			got++
		}
	}
	if got != want {
		t.Errorf("%s creates = %d, want %d", resource, got, want)
	}
}
