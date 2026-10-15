// kube.go — small Kubernetes helpers for the e2e scenario: polling with
// progress logs, applying a manifest with the VM's kubectl, running the
// client image as a one-shot pod, and reading pod logs.
package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	clientImage    = "ghcr.io/aquaduct-dev/weft-operator-e2e-client:e2e"
	clientNS       = "e2e-client"
	pollInterval   = 2 * time.Second
	progressPeriod = 15 * time.Second
)

// waitFor polls check until it reports done, an error, or timeout. check
// returns a short state description that is logged periodically and
// included in the timeout error.
func waitFor(ctx context.Context, timeout time.Duration, what string, check func() (bool, string, error)) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	start := time.Now()
	lastLog := time.Time{}
	state := ""
	for {
		done, s, err := check()
		state = s
		if err != nil {
			return fmt.Errorf("%s: %w", what, err)
		}
		if done {
			log.Printf("ok: %s (%s, %s)", what, state, time.Since(start).Round(time.Second))
			return nil
		}
		if time.Since(lastLog) > progressPeriod {
			log.Printf("waiting: %s (%s, %s)", what, state, time.Since(start).Round(time.Second))
			lastLog = time.Now()
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("%s: timed out after %s; last state: %s", what, timeout, state)
		case <-time.After(pollInterval):
		}
	}
}

// kubectlApply applies a manifest file into namespace with the VM's kubectl,
// server-side so large CRDs do not overflow the last-applied annotation.
func (s *scenario) kubectlApply(ctx context.Context, path, namespace string) error {
	cmd := exec.CommandContext(ctx, "/usr/local/bin/kubectl", "apply", "--server-side", "--force-conflicts", "-n", namespace, "-f", path)
	cmd.Env = append(os.Environ(), "KUBECONFIG="+s.kubeconfig)
	out, err := cmd.CombinedOutput()
	log.Printf("kubectl apply -n %s -f %s:\n%s", namespace, path, out)
	if err != nil {
		return fmt.Errorf("kubectl apply %s: %w", path, err)
	}
	return nil
}

// podLogs returns a container's full log.
func (s *scenario) podLogs(ctx context.Context, namespace, name string) (string, error) {
	rc, err := s.clientset.CoreV1().Pods(namespace).GetLogs(name, &corev1.PodLogOptions{}).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	b, err := io.ReadAll(rc)
	return string(b), err
}

// runClientPod runs the client image once with args and returns its logs.
// It fails if the pod does not exit 0.
func (s *scenario) runClientPod(ctx context.Context, name string, args ...string) (string, error) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Namespace: clientNS, Name: name},
		Spec: corev1.PodSpec{
			RestartPolicy:                corev1.RestartPolicyNever,
			AutomountServiceAccountToken: ptr(false),
			Containers: []corev1.Container{{
				Name:            "client",
				Image:           clientImage,
				ImagePullPolicy: corev1.PullNever,
				Args:            append([]string{"client"}, args...),
			}},
		},
	}
	_ = s.c.Delete(ctx, pod)
	if err := waitFor(ctx, time.Minute, "old pod "+name+" gone", func() (bool, string, error) {
		err := s.c.Get(ctx, types.NamespacedName{Namespace: clientNS, Name: name}, &corev1.Pod{})
		return apierrors.IsNotFound(err), "deleting", nil
	}); err != nil {
		return "", err
	}
	if err := s.c.Create(ctx, pod); err != nil {
		return "", fmt.Errorf("create client pod: %w", err)
	}
	var phase corev1.PodPhase
	err := waitFor(ctx, 4*time.Minute, "client pod "+name+" finished", func() (bool, string, error) {
		p := &corev1.Pod{}
		if err := s.c.Get(ctx, types.NamespacedName{Namespace: clientNS, Name: name}, p); err != nil {
			return false, "", err
		}
		phase = p.Status.Phase
		return phase == corev1.PodSucceeded || phase == corev1.PodFailed, string(phase) + " " + containerState(p), nil
	})
	logs, lerr := s.podLogs(ctx, clientNS, name)
	log.Printf("client pod %s logs:\n%s", name, logs)
	if err != nil {
		return logs, err
	}
	if lerr != nil {
		return logs, lerr
	}
	if phase != corev1.PodSucceeded {
		return logs, fmt.Errorf("client pod %s ended %s", name, phase)
	}
	return logs, nil
}

// containerState summarizes why a pod's first container is not running.
func containerState(p *corev1.Pod) string {
	for _, cs := range p.Status.ContainerStatuses {
		switch {
		case cs.State.Waiting != nil:
			return cs.State.Waiting.Reason + ": " + cs.State.Waiting.Message
		case cs.State.Terminated != nil:
			return fmt.Sprintf("terminated %d %s", cs.State.Terminated.ExitCode, cs.State.Terminated.Reason)
		case cs.State.Running != nil:
			return "running"
		}
	}
	return "no container status"
}

func ptr[T any](v T) *T { return &v }
