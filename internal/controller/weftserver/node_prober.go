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
	"io"
	"strings"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
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
	Client    client.Client
	Scheme    *runtime.Scheme
	Clientset kubernetes.Interface
}

//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete

// Start implements manager.Runnable
func (p *NodeProber) Start(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("node-prober")
	log.Info("Starting Node Prober")

	// Initial run
	if err := p.probeNodes(ctx); err != nil {
		log.Error(err, "Failed to run initial probe cycle")
	}
	if err := p.cleanupOrphanedServers(ctx); err != nil {
		log.Error(err, "Failed to run initial cleanup cycle")
	}

	ticker := time.NewTicker(ProbeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := p.probeNodes(ctx); err != nil {
				log.Error(err, "Failed to run probe cycle")
			}
			if err := p.cleanupOrphanedServers(ctx); err != nil {
				log.Error(err, "Failed to run cleanup cycle")
			}
		case <-ctx.Done():
			log.Info("Stopping Node Prober")
			return nil
		}
	}
}

func (p *NodeProber) cleanupOrphanedServers(ctx context.Context) error {
	log := log.FromContext(ctx).WithName("node-prober")

	// List all nodes first for efficiency
	var nodeList corev1.NodeList
	if err := p.Client.List(ctx, &nodeList); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}
	existingNodes := make(map[string]bool)
	for _, node := range nodeList.Items {
		existingNodes[node.Name] = true
	}

	var wsList weftv1alpha1.WeftServerList
	if err := p.Client.List(ctx, &wsList); err != nil {
		return fmt.Errorf("failed to list WeftServers: %w", err)
	}

	for _, ws := range wsList.Items {
		nodeName, ok := ws.Labels["weft.aquaduct.dev/node"]
		if !ok {
			continue
		}

		if !existingNodes[nodeName] {
			log.Info("Deleting WeftServer for missing node", "weftServer", ws.Name, "namespace", ws.Namespace, "node", nodeName)
			if err := p.Client.Delete(ctx, &ws); err != nil {
				log.Error(err, "Failed to delete WeftServer", "weftServer", ws.Name, "namespace", ws.Namespace)
			}
		}
	}
	return nil
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
	backoff := int32(9)
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
							Args: []string{
								"probe",
								"--bind-ip", ip,
							},
							ImagePullPolicy: corev1.PullAlways,
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
	for range 60 { // Wait up to 60 seconds (probe should be fast)
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

		// Read logs to discover public IP
		// Find Pod for Job
		var podList corev1.PodList
		// Job creates pods with label "job-name=<jobName>" or "controller-uid=<jobUID>"
		// batch/v1 Job controller uses "controller-uid" for Pods owner ref and label "job-name" usually.
		// Let's try matching labels.
		var updatedJob batchv1.Job
		if err := p.Client.Get(ctx, types.NamespacedName{Name: jobName, Namespace: TargetNamespace}, &updatedJob); err == nil {
			// Use selector from job spec if available, or match labels
			if err := p.Client.List(ctx, &podList, client.InNamespace(TargetNamespace), client.MatchingLabels{"job-name": jobName}); err == nil && len(podList.Items) > 0 {
				podName := podList.Items[0].Name
				req := p.Clientset.CoreV1().Pods(TargetNamespace).GetLogs(podName, &corev1.PodLogOptions{})
				podLogs, err := req.Stream(ctx)
				if err == nil {
					defer podLogs.Close()
					buf := new(strings.Builder)
					_, err := io.Copy(buf, podLogs)
					if err == nil {
						logs := buf.String()
						// Parse IP from logs. Assuming format "Public IP: <ip>" or similar?
						// User said "read the logs from the job to discover the node's public IP address".
						// I need to know the log format.
						// Assuming `weft probe` outputs the IP as the last line or something.
						// Or I can assume it just prints the IP.
						// Let's assume the log contains the IP.
						// For now, let's try to find an IP address in the logs.
						// Or maybe `weft probe` outputs JSON?
						// Without `weft probe` source, I'll guess.
						// Assuming `weft probe` prints the IP to stdout.
						trimmed := strings.TrimSpace(logs)
						if trimmed != "" {
							// Basic validation?
							ip = trimmed
							log.Info("Discovered public IP from logs", "ip", ip)
						}
					}
				}
			}
		}

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
