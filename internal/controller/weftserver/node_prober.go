/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package weftserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

const (
	// TODO: Make this configurable or discoverable
	TargetNamespace = "weft-system"
	ProbeInterval   = 3 * time.Hour
)

// NodeProber periodically probes nodes to see if they are suitable for WeftServers
type NodeProber struct {
	Client client.Client
	Scheme *runtime.Scheme
}

// Start implements manager.Runnable
func (p *NodeProber) Start(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("node-prober")
	log.Info("Starting Node Prober")

	// Initial run
	if err := p.probeNodes(ctx); err != nil {
		log.Error(err, "Failed to run initial probe cycle")
	}

	ticker := time.NewTicker(ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.probeNodes(ctx); err != nil {
				log.Error(err, "Failed to run probe cycle")
			}
		case <-ctx.Done():
			log.Info("Stopping Node Prober")
			return nil
		}
	}
}

func (p *NodeProber) probeNodes(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("node-prober")

	var nodeList corev1.NodeList
	if err := p.Client.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	for _, node := range nodeList.Items {
		if err := p.processNode(ctx, &node); err != nil {
			log.Error(err, "Failed to process node", "node", node.Name)
			// Continue with other nodes
		}
	}
	return nil
}

func (p *NodeProber) processNode(ctx context.Context, node *corev1.Node) error {
	log := log.FromContext(ctx).WithName("node-prober").WithValues("node", node.Name)

	// Check if WeftServer already exists for this node
	// Naming convention: node-<node-name>
	// We also check if we created it
	serverName := fmt.Sprintf("node-%s", node.Name)
	var ws weftv1alpha1.WeftServer
	err := p.Client.Get(ctx, types.NamespacedName{Name: serverName, Namespace: TargetNamespace}, &ws)
	if err == nil {
		// Already exists
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}

	// Determine IP
	ip := getNodeIP(node)
	if ip == "" {
		log.Info("Skipping node with no valid IP")
		return nil
	}

	// Create Probe Job
	jobName := fmt.Sprintf("weft-probe-%s-%s", node.Name, randString(4))
	ttl := int32(3600)
	backoff := int32(0)
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: TargetNamespace,
			Labels: map[string]string{
				"app": "weft-probe",
			},
		},
		Spec: batchv1.JobSpec{
			TTLSecondsAfterFinished: &ttl,
			BackoffLimit:            &backoff,
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					NodeName:      node.Name,
					HostNetwork:   true,
					Containers: []corev1.Container{
						{
							Name:  "probe",
							Image: "ghcr.io/aquaduct-dev/weft:latest", // TODO: Versioning
							Command: []string{
								"/weft.runfiles/_main/weft", "probe",
								"--bind-ip", ip,
							},
						},
					},
				},
			},
		},
	}

	log.Info("Creating probe job", "job", jobName, "ip", ip)
	if err := p.Client.Create(ctx, job); err != nil {
		return fmt.Errorf("failed to create probe job: %w", err)
	}

	// Wait for Job completion
	// Use a simple poll loop
	success := false
	for i := 0; i < 60; i++ { // Wait up to 60 seconds (probe should be fast)
		time.Sleep(1 * time.Second)
		var updatedJob batchv1.Job
		if err := p.Client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: TargetNamespace}, &updatedJob); err != nil {
			log.Error(err, "Failed to get probe job")
			continue
		}

		if updatedJob.Status.Succeeded > 0 {
			success = true
			break
		}
		if updatedJob.Status.Failed > 0 {
			log.Info("Probe failed", "job", jobName)
			break
		}
	}

	if success {
		log.Info("Probe succeeded, creating WeftServer")
		// Create WeftServer
		secret := randString(10)
		connStr := fmt.Sprintf("weft://%s@%s:9092", secret, ip)

		newWS := &weftv1alpha1.WeftServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serverName,
				Namespace: TargetNamespace,
				Labels: map[string]string{
					"weft.aquaduct.dev/auto-created": "true",
					"weft.aquaduct.dev/node":         node.Name,
				},
			},
			Spec: weftv1alpha1.WeftServerSpec{
				ConnectionString: connStr,
				// BindInterface: ... optional
			},
		}

		if err := p.Client.Create(ctx, newWS); err != nil {
			return fmt.Errorf("failed to create WeftServer: %w", err)
		}
	}

	return nil
}

func getNodeIP(node *corev1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeExternalIP {
			return addr.Address
		}
	}
	return ""
}

func randString(n int) string {
	b := make([]byte, n/2+1) // hex is 2 chars per byte
	if _, err := rand.Read(b); err != nil {
		// Should not happen
		return "random"
	}
	return hex.EncodeToString(b)[:n]
}
