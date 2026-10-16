// default_scenario.go — the "default-reflector" scenario
// (//e2e/reflector:default_reflector_test). The operator is installed with
// the chart's default values, so nobody creates a WeftLocalDNSReflector by
// hand; the operator's cluster-default reflector must pick up the tunnelled
// registry hostname on its own and serve it in-cluster. Re-installing with
// defaultReflector.enabled=false must remove it and its DNS override again.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
)

const (
	defaultReflectorName = "cluster-default"
	// operatorDisabledManifest is the same chart rendered with
	// defaultReflector.enabled=false.
	operatorDisabledManifest = "/etc/e2e/weft-operator-default-disabled.yaml"
)

func (s *scenario) defaultReflectorSteps() []step {
	return []step{
		{"e2e.operator-ready", "operator installed from the chart with default values", s.installOperator},
		{"e2e.default-created", "operator created the cluster-default allHostnames reflector", s.checkDefaultCreated},
		{"e2e.backend-ready", "registry backend running and published by a WeftTunnel", s.deployBackendAndTunnel},
		{"e2e.default-reflects", "cluster-default serves the tunnelled hostname in-cluster with a trusted certificate", s.checkDefaultReflects},
		{"e2e.default-disabled", "defaultReflector.enabled=false removes the reflector and its DNS override", s.checkDefaultDisabled},
	}
}

func (s *scenario) checkDefaultCreated(ctx context.Context) error {
	if err := waitFor(ctx, 3*time.Minute, "cluster-default reflector created by the operator", func() (bool, string, error) {
		r, err := s.reflector(ctx)
		if apierrors.IsNotFound(err) {
			return false, "absent", nil
		}
		if err != nil {
			return false, "", err
		}
		ok := r.Spec.AllHostnames && r.Labels["weft.aquaduct.dev/default-reflector"] == "true"
		return ok, fmt.Sprintf("allHostnames=%v labels=%v", r.Spec.AllHostnames, r.Labels), nil
	}); err != nil {
		return err
	}
	// Pods created from here on must be admitted with its CA.
	return waitFor(ctx, 2*time.Minute, "cluster-default CA issued", func() (bool, string, error) {
		err := s.c.Get(ctx, types.NamespacedName{Namespace: operatorNS, Name: s.refl + "-ca"}, &corev1.Secret{})
		return err == nil, fmt.Sprintf("get err=%v", err), nil
	})
}

func (s *scenario) deployBackendAndTunnel(ctx context.Context) error {
	if err := s.deployBackend(ctx); err != nil {
		return err
	}
	return s.createTunnel(ctx)
}

func (s *scenario) checkDefaultReflects(ctx context.Context) error {
	if err := waitFor(ctx, 5*time.Minute, "cluster-default Ready and reflecting the registry hostname", func() (bool, string, error) {
		r, err := s.reflector(ctx)
		if err != nil {
			return false, "", err
		}
		s.serviceIP = r.Status.ServiceIP
		claimed := slices.Contains(r.Status.ReflectedHostnames, hostname)
		ready := meta.IsStatusConditionTrue(r.Status.Conditions, "Ready")
		return claimed && ready, fmt.Sprintf("reflected=%v ready=%v untrusted=%d", r.Status.ReflectedHostnames, ready, r.Status.UntrustedPods), nil
	}); err != nil {
		return err
	}
	var lastErr error
	for attempt := 1; attempt <= 8; attempt++ {
		if _, lastErr = s.runClientPod(ctx, "default-probe", "--host="+hostname, "--expect-ip="+s.serviceIP); lastErr == nil {
			return nil
		}
		log.Printf("default probe attempt %d failed: %v; retrying", attempt, lastErr)
		time.Sleep(15 * time.Second)
	}
	return lastErr
}

func (s *scenario) checkDefaultDisabled(ctx context.Context) error {
	if err := s.kubectlApply(ctx, operatorDisabledManifest, operatorNS); err != nil {
		return err
	}
	if err := waitFor(ctx, 5*time.Minute, "cluster-default reflector, its DNS override and node entry removed", func() (bool, string, error) {
		_, err := s.reflector(ctx)
		gone := apierrors.IsNotFound(err)
		dns := s.coreDNSHasOverride(ctx)
		hosts, _ := os.ReadFile(nodeHostsFile)
		hostsLeft := strings.Contains(string(hosts), hostname)
		return gone && !dns && !hostsLeft, fmt.Sprintf("reflector-gone=%v dns=%v hosts=%v", gone, dns, hostsLeft), nil
	}); err != nil {
		return err
	}
	// CoreDNS drops the block on its next reload; retry until the name no
	// longer resolves.
	var lastErr error
	for attempt := 1; attempt <= 8; attempt++ {
		if _, lastErr = s.runClientPod(ctx, "default-after", "--host="+hostname, "--expect-unresolvable"); lastErr == nil {
			return nil
		}
		log.Printf("still resolvable (attempt %d): %v; retrying", attempt, lastErr)
		time.Sleep(15 * time.Second)
	}
	return lastErr
}
