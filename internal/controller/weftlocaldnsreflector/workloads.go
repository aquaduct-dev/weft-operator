// workloads.go — the Kubernetes objects that carry a WeftLocalDNSReflector's
// traffic: the `weft proxy` Deployment that terminates TLS, the ClusterIP
// Service DNS points at, and the node agent DaemonSet that wires containerd
// pulls to that Service. Each ensure* mutates only the fields it owns so the
// API server's defaults survive and CreateOrUpdate stays a no-op at rest.
package weftlocaldnsreflector

import (
	"context"
	"fmt"
	"path"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/trust"
)

const (
	componentProxy     = "proxy"
	componentNodeAgent = "node-agent"
)

// ensureProxy reconciles the proxy Deployment. With no routes it scales to
// zero: `weft proxy` refuses to start without at least one pair.
func (r *Reconciler) ensureProxy(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector, routes routeSet, fingerprint string) (*appsv1.Deployment, error) {
	logger := log.FromContext(ctx)
	dep := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Namespace: refl.Spec.Namespace, Name: proxyName(refl)}}

	replicas := int32(1)
	if refl.Spec.Replicas != nil {
		replicas = *refl.Spec.Replicas
	}
	if len(routes.Routes) == 0 {
		replicas = 0
	}
	probePort := proxyHTTPSPort
	if !routes.HasScheme("https") {
		probePort = proxyHTTPPort
	}

	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, dep, func() error {
		dep.Labels = managedLabels(refl, componentProxy)
		if dep.CreationTimestamp.IsZero() {
			dep.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels(refl, componentProxy)}
		}
		dep.Spec.Replicas = &replicas
		tmpl := &dep.Spec.Template
		tmpl.Labels = managedLabels(refl, componentProxy)
		if tmpl.Annotations == nil {
			tmpl.Annotations = map[string]string{}
		}
		tmpl.Annotations[certFingerprintAnnotation] = fingerprint
		tmpl.Spec.AutomountServiceAccountToken = ptr(false)

		c := containerByName(&tmpl.Spec.Containers, "proxy")
		c.Image = r.WeftImage
		c.ImagePullPolicy = pullPolicy(r.WeftImage)
		c.Args = proxyArgs(routes.Routes, path.Join(tlsMountPath, corev1.TLSCertKey), path.Join(tlsMountPath, corev1.TLSPrivateKeyKey))
		c.Ports = []corev1.ContainerPort{
			{Name: "https", ContainerPort: proxyHTTPSPort, Protocol: corev1.ProtocolTCP},
			{Name: "http", ContainerPort: proxyHTTPPort, Protocol: corev1.ProtocolTCP},
		}
		c.ReadinessProbe = tcpProbe(probePort, c.ReadinessProbe)
		c.VolumeMounts = []corev1.VolumeMount{{Name: "tls", MountPath: tlsMountPath, ReadOnly: true}}
		c.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("20m"), corev1.ResourceMemory: resource.MustParse("32Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("256Mi")},
		}
		setVolume(&tmpl.Spec.Volumes, corev1.Volume{Name: "tls", VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{SecretName: tlsSecretName(refl), DefaultMode: ptr(int32(0o440))},
		}})
		return controllerutil.SetControllerReference(refl, dep, r.Scheme)
	})
	if err != nil {
		return nil, fmt.Errorf("reconcile proxy deployment: %w", err)
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Proxy deployment reconciled", "deployment", dep.Namespace+"/"+dep.Name, "operation", op, "replicas", replicas, "routes", len(routes.Routes))
	}
	return dep, nil
}

// ensureService reconciles the ClusterIP Service fronting the proxy and
// returns its ClusterIP ("" until the API server allocates one).
func (r *Reconciler) ensureService(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector) (string, error) {
	logger := log.FromContext(ctx)
	svc := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Namespace: refl.Spec.Namespace, Name: proxyName(refl)}}
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, svc, func() error {
		svc.Labels = managedLabels(refl, componentProxy)
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Spec.Selector = selectorLabels(refl, componentProxy)
		svc.Spec.Ports = mergeServicePorts(svc.Spec.Ports, []corev1.ServicePort{
			{Name: "https", Port: 443, TargetPort: intstr.FromString("https"), Protocol: corev1.ProtocolTCP},
			{Name: "http", Port: 80, TargetPort: intstr.FromString("http"), Protocol: corev1.ProtocolTCP},
		})
		return controllerutil.SetControllerReference(refl, svc, r.Scheme)
	})
	if err != nil {
		return "", fmt.Errorf("reconcile proxy service: %w", err)
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Proxy service reconciled", "service", svc.Namespace+"/"+svc.Name, "operation", op, "clusterIP", svc.Spec.ClusterIP)
	}
	if svc.Spec.ClusterIP == corev1.ClusterIPNone {
		return "", nil
	}
	return svc.Spec.ClusterIP, nil
}

// ensureNodeAgent reconciles the node agent DaemonSet, or deletes it when
// node trust is disabled or there is nothing to point nodes at yet. Deleting
// it makes each agent remove its /etc/hosts entry on SIGTERM.
func (r *Reconciler) ensureNodeAgent(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector, serviceIP string, hostnames []string) error {
	logger := log.FromContext(ctx)
	ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: refl.Spec.Namespace, Name: nodeAgentName(refl)}}

	if refl.Spec.NodeTrust.Disabled || serviceIP == "" || len(hostnames) == 0 {
		if err := r.Delete(ctx, ds); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete node agent: %w", err)
		} else if err == nil {
			logger.Info("Node agent removed", "daemonset", ds.Namespace+"/"+ds.Name, "disabled", refl.Spec.NodeTrust.Disabled, "serviceIP", serviceIP, "hostnames", len(hostnames))
		}
		return nil
	}
	if r.OperatorImage == "" {
		return fmt.Errorf("node agent needs the operator image: set WEFT_OPERATOR_IMAGE on the operator Deployment")
	}

	certsDir := refl.Spec.NodeTrust.ContainerdCertsDir
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, ds, func() error {
		ds.Labels = managedLabels(refl, componentNodeAgent)
		if ds.CreationTimestamp.IsZero() {
			ds.Spec.Selector = &metav1.LabelSelector{MatchLabels: selectorLabels(refl, componentNodeAgent)}
		}
		tmpl := &ds.Spec.Template
		tmpl.Labels = managedLabels(refl, componentNodeAgent)
		spec := &tmpl.Spec
		// Host network so the TLS health check takes the same path to the
		// ClusterIP that containerd will.
		spec.HostNetwork = true
		spec.DNSPolicy = corev1.DNSClusterFirstWithHostNet
		spec.AutomountServiceAccountToken = ptr(false)
		spec.TerminationGracePeriodSeconds = ptr(int64(30))
		spec.Tolerations = []corev1.Toleration{{Operator: corev1.TolerationOpExists}}
		spec.PriorityClassName = "system-node-critical"

		c := containerByName(&spec.Containers, "agent")
		c.Image = r.OperatorImage
		c.ImagePullPolicy = pullPolicy(r.OperatorImage)
		c.Args = []string{
			"node-agent",
			"--reflector=" + refl.Name,
			"--service-ip=" + serviceIP,
			"--hostnames=" + strings.Join(hostnames, ","),
			"--ca-file=" + path.Join(caMountPath, trust.CAsKey),
			"--hosts-file=" + path.Join(hostEtcPath, "hosts"),
			"--host-trust-files=" + strings.Join(hostTrustFiles(), ","),
			"--certs-dir=" + certsDir,
		}
		// Root plus DAC_OVERRIDE to edit host files that may not be owned
		// by root (e.g. images built by an unprivileged rootfs builder);
		// nothing else.
		c.SecurityContext = &corev1.SecurityContext{
			RunAsUser:                ptr(int64(0)),
			RunAsNonRoot:             ptr(false),
			AllowPrivilegeEscalation: ptr(false),
			ReadOnlyRootFilesystem:   ptr(true),
			Capabilities:             &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"DAC_OVERRIDE"}},
		}
		c.Resources = corev1.ResourceRequirements{
			Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("5m"), corev1.ResourceMemory: resource.MustParse("16Mi")},
			Limits:   corev1.ResourceList{corev1.ResourceMemory: resource.MustParse("64Mi")},
		}
		c.VolumeMounts = []corev1.VolumeMount{
			// The node's /etc: hosts, plus the distro CA bundles host
			// processes trust.
			{Name: "host-etc", MountPath: hostEtcPath},
			// Same path inside and out: hosts.toml references the CA by
			// its host path, which containerd resolves.
			{Name: "certs-d", MountPath: certsDir},
			{Name: "ca", MountPath: caMountPath, ReadOnly: true},
		}
		removeVolume(&spec.Volumes, "hosts")
		setVolume(&spec.Volumes, corev1.Volume{Name: "host-etc", VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: "/etc", Type: ptr(corev1.HostPathDirectory)},
		}})
		setVolume(&spec.Volumes, corev1.Volume{Name: "certs-d", VolumeSource: corev1.VolumeSource{
			HostPath: &corev1.HostPathVolumeSource{Path: certsDir, Type: ptr(corev1.HostPathDirectoryOrCreate)},
		}})
		setVolume(&spec.Volumes, corev1.Volume{Name: "ca", VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: trust.ConfigMapName}},
		}})
		return controllerutil.SetControllerReference(refl, ds, r.Scheme)
	})
	if err != nil {
		return fmt.Errorf("reconcile node agent: %w", err)
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Node agent reconciled", "daemonset", ds.Namespace+"/"+ds.Name, "operation", op, "serviceIP", serviceIP, "hostnames", hostnames)
	}
	return nil
}

// hostTrustFiles are the node's distro CA bundles (as seen by the agent)
// that get a managed block with the reflector CAs; missing ones are skipped.
func hostTrustFiles() []string {
	return []string{
		path.Join(hostEtcPath, "ssl/certs/ca-certificates.crt"),
		path.Join(hostEtcPath, "pki/tls/certs/ca-bundle.crt"),
	}
}

// removeVolume drops the named volume if present (used when a volume is
// renamed between operator versions).
func removeVolume(volumes *[]corev1.Volume, name string) {
	out := (*volumes)[:0]
	for _, v := range *volumes {
		if v.Name != name {
			out = append(out, v)
		}
	}
	*volumes = out
}

// pullPolicy mirrors the API server's default: always re-pull a floating
// :latest (or untagged) image, otherwise use what the node has. Setting it
// explicitly keeps CreateOrUpdate idempotent; Always for a pinned tag would
// also make the proxy unrunnable on airgapped nodes.
func pullPolicy(image string) corev1.PullPolicy {
	if strings.Contains(image, "@") {
		return corev1.PullIfNotPresent
	}
	name := image[strings.LastIndex(image, "/")+1:]
	if i := strings.LastIndex(name, ":"); i < 0 || name[i+1:] == "latest" {
		return corev1.PullAlways
	}
	return corev1.PullIfNotPresent
}

// tcpProbe returns a TCP readiness probe on port, keeping any server-set
// defaults from the existing probe so the update stays idempotent.
func tcpProbe(port int, existing *corev1.Probe) *corev1.Probe {
	p := existing
	if p == nil {
		p = &corev1.Probe{}
	}
	p.ProbeHandler = corev1.ProbeHandler{TCPSocket: &corev1.TCPSocketAction{Port: intstr.FromInt(port)}}
	p.PeriodSeconds = 5
	return p
}

// mergeServicePorts sets name/port/targetPort/protocol for each wanted port
// while keeping fields the API server filled in, and drops any others.
func mergeServicePorts(existing, wanted []corev1.ServicePort) []corev1.ServicePort {
	out := make([]corev1.ServicePort, 0, len(wanted))
	for _, w := range wanted {
		for _, e := range existing {
			if e.Name == w.Name {
				e.Port, e.TargetPort, e.Protocol = w.Port, w.TargetPort, w.Protocol
				w = e
				break
			}
		}
		out = append(out, w)
	}
	return out
}

// containerByName returns the named container, appending it if missing.
func containerByName(containers *[]corev1.Container, name string) *corev1.Container {
	for i := range *containers {
		if (*containers)[i].Name == name {
			return &(*containers)[i]
		}
	}
	*containers = append(*containers, corev1.Container{Name: name})
	return &(*containers)[len(*containers)-1]
}

// setVolume replaces the named volume's source, appending it if missing.
// Server defaults on the source (e.g. defaultMode) are kept when the
// replacement leaves them unset.
func setVolume(volumes *[]corev1.Volume, v corev1.Volume) {
	for i := range *volumes {
		if (*volumes)[i].Name != v.Name {
			continue
		}
		old := (*volumes)[i].VolumeSource
		switch {
		case v.ConfigMap != nil && old.ConfigMap != nil && v.ConfigMap.DefaultMode == nil:
			v.ConfigMap.DefaultMode = old.ConfigMap.DefaultMode
		case v.Secret != nil && old.Secret != nil && v.Secret.DefaultMode == nil:
			v.Secret.DefaultMode = old.Secret.DefaultMode
		}
		(*volumes)[i] = v
		return
	}
	*volumes = append(*volumes, v)
}

// deploymentAvailable reports whether at least one proxy replica is ready.
func deploymentAvailable(dep *appsv1.Deployment) bool {
	return dep != nil && dep.Status.AvailableReplicas > 0
}

func ptr[T any](v T) *T { return &v }
