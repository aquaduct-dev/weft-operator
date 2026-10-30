// Package trustinjector is the mutating admission webhook that makes every
// new pod trust the WeftLocalDNSReflector CAs (see internal/trust). It is
// registered with failurePolicy Ignore: if the operator is down, pods are
// created untouched, and the reflector's trust gate withdraws its DNS
// override until those pods are replaced, so nothing breaks.
//
// handler.go is the admission handler; setup.go manages its serving
// certificate and the MutatingWebhookConfiguration.
package trustinjector

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"aquaduct.dev/weft-operator/internal/trust"
)

// Path is where the handler is served on the manager's webhook server.
const Path = "/inject-trust"

// Handler injects the trust bundle into pods.
type Handler struct {
	// Client creates the namespace's bundle ConfigMap if the reconciler has
	// not published it yet (e.g. the namespace was created moments ago).
	Client  client.Client
	Source  *trust.Source
	Decoder admission.Decoder
}

// Handle implements admission.Handler. It never denies: any problem results
// in an unmodified pod and a log line.
func (h *Handler) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithValues("namespace", req.Namespace, "pod", podName(req))

	pod := &corev1.Pod{}
	if err := h.Decoder.Decode(req, pod); err != nil {
		logger.Error(err, "Cannot decode pod; admitting unmodified")
		return admission.Allowed("undecodable pod")
	}
	if trust.OptedOut(pod) {
		return admission.Allowed("opted out")
	}

	bundle, err := h.Source.Build(ctx)
	if err != nil {
		logger.Error(err, "Cannot build trust bundle; admitting unmodified")
		return admission.Allowed("trust bundle unavailable")
	}
	if bundle.Empty() {
		return admission.Allowed("no reflector CAs")
	}

	// The pod will mount the namespace's bundle ConfigMap; make sure it
	// exists so the pod is not stuck in ContainerCreating. Dry runs only
	// check it (sideEffects: NoneOnDryRun).
	if req.DryRun == nil || !*req.DryRun {
		created, err := h.Source.CreateConfigMapIfMissing(ctx, h.Client, req.Namespace, bundle)
		if errors.Is(err, trust.ErrForeignConfigMap) {
			return foreign(logger)
		}
		if err != nil {
			logger.Error(err, "Cannot publish trust bundle in namespace; admitting unmodified")
			return admission.Allowed("trust bundle not publishable")
		}
		if created {
			logger.Info("Published trust bundle on demand", "configmap", trust.ConfigMapName)
		}
	} else if err := h.Source.CheckConfigMap(ctx, h.Client, req.Namespace); errors.Is(err, trust.ErrForeignConfigMap) {
		return foreign(logger)
	} else if err != nil {
		logger.Error(err, "Cannot check trust bundle in namespace; admitting unmodified")
		return admission.Allowed("trust bundle not checkable")
	}

	if !trust.InjectPod(pod, bundle) {
		return admission.Allowed("nothing to inject")
	}
	marshaled, err := json.Marshal(pod)
	if err != nil {
		logger.Error(err, "Cannot marshal mutated pod; admitting unmodified")
		return admission.Allowed("marshal failed")
	}
	logger.V(1).Info("Injected trust bundle", "cas", trust.FingerprintList(bundle.Fingerprints))
	return admission.PatchResponseFromRaw(req.Object.Raw, marshaled)
}

// foreign admits a pod unmodified because its namespace's bundle name is
// taken by someone else's ConfigMap. The pod then does not trust the CA and
// the reflector trust gate counts it, which is the safe outcome.
func foreign(logger logr.Logger) admission.Response {
	logger.Info("Namespace has an unrelated ConfigMap named like the trust bundle; admitting pod without trust injection", "configmap", trust.ConfigMapName)
	return admission.Allowed("trust bundle name taken by an unrelated ConfigMap")
}

func podName(req admission.Request) string {
	if req.Name != "" {
		return req.Name
	}
	var meta struct {
		Metadata struct {
			GenerateName string `json:"generateName"`
		} `json:"metadata"`
	}
	_ = json.Unmarshal(req.Object.Raw, &meta)
	return meta.Metadata.GenerateName + "*"
}

var _ admission.Handler = (*Handler)(nil)
