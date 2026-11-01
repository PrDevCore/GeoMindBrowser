package main

import (
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"geomind/session/internal/kube"
	"geomind/session/internal/session"
)

// buildRuntime selects the session runtime from configuration. The fake
// runtime is the default so the control plane stays runnable without a
// cluster; the Kubernetes runtime is opt-in.
func buildRuntime(cfg config) (session.Runtime, error) {
	switch cfg.Runtime {
	case runtimeFake:
		return session.NewFakeRuntime(), nil
	case runtimeKube:
		return newKubeRuntime(cfg)
	default:
		return nil, fmt.Errorf("unknown session runtime %q", cfg.Runtime)
	}
}

func newKubeRuntime(cfg config) (session.Runtime, error) {
	restCfg, err := kubeRESTConfig(cfg.KubeConfig)
	if err != nil {
		return nil, err
	}
	port, err := safePort("GEOMIND_SESSION_KUBE_PROXY_PORT", cfg.KubeProxyPort)
	if err != nil {
		return nil, err
	}
	return kube.New(restCfg, kube.Options{
		Namespace:      cfg.KubeNamespace,
		Image:          cfg.KubeImage,
		ProxyNamespace: cfg.KubeProxyNS,
		ProxyPort:      port,
		EdgeNamespace:  cfg.KubeEdgeNS,
		ReadyTimeout:   cfg.KubeReadyTimeout,
		PollInterval:   cfg.KubePollInterval,
	})
}

func kubeRESTConfig(path string) (*rest.Config, error) {
	if path != "" {
		restCfg, err := clientcmd.BuildConfigFromFlags("", path)
		if err != nil {
			return nil, fmt.Errorf("kubeconfig: %w", err)
		}
		return restCfg, nil
	}
	restCfg, err := rest.InClusterConfig()
	if err != nil {
		return nil, fmt.Errorf("in-cluster config: %w", err)
	}
	return restCfg, nil
}

func safePort(field string, value int) (int32, error) {
	if value < 1 || value > 65535 {
		return 0, fmt.Errorf("%s %d is out of range", field, value)
	}
	return int32(value), nil
}
