// setup.go — everything the trust-injection webhook needs besides its
// handler: a serving certificate that survives operator restarts (kept in a
// Secret, so the API server's caBundle stays valid across restarts and the
// webhook is never briefly untrusted), and the MutatingWebhookConfiguration
// pointing the API server at the operator's webhook Service.
package trustinjector

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"aquaduct.dev/weft-operator/internal/pki"
	"aquaduct.dev/weft-operator/internal/trust"
)

const (
	// ConfigurationName is the cluster-scoped MutatingWebhookConfiguration.
	ConfigurationName = "weft-operator-trust-injector"
	// SecretName holds the webhook CA and serving certificate.
	SecretName = "weft-operator-webhook-tls"

	webhookName = "trust.weft.aquaduct.dev"
	caCertKey   = "ca.crt"
	caKeyKey    = "ca.key"
)

// Config says where the webhook is reachable.
type Config struct {
	// Namespace and ServiceName locate the operator's webhook Service.
	Namespace   string
	ServiceName string
	// CertDir is the manager webhook server's cert directory; tls.crt and
	// tls.key are written there.
	CertDir string
	// ExcludedNamespaces never get injected (and are exempt from the
	// reflector trust gate).
	ExcludedNamespaces []string
}

// DNSName is the name the API server dials.
func (c Config) DNSName() string { return fmt.Sprintf("%s.%s.svc", c.ServiceName, c.Namespace) }

//+kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

// EnsureServingCert loads (or creates, or renews) the webhook certificate
// Secret, writes tls.crt/tls.key into cfg.CertDir and returns the CA PEM for
// the webhook configuration's caBundle. c must be a live (uncached) client:
// this runs before the manager's caches start.
func EnsureServingCert(ctx context.Context, c client.Client, cfg Config, now time.Time) ([]byte, error) {
	logger := log.FromContext(ctx).WithValues("secret", cfg.Namespace+"/"+SecretName)
	secret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: cfg.Namespace, Name: SecretName}}
	op, err := controllerutil.CreateOrUpdate(ctx, c, secret, func() error {
		if secret.Data == nil {
			secret.Data = map[string][]byte{}
		}
		if _, err := pki.ParseCert(secret.Data[caCertKey]); err != nil || len(secret.Data[caKeyKey]) == 0 {
			logger.Info("Generating webhook CA")
			caCert, caKey, err := pki.NewCA("weft-operator webhook", now)
			if err != nil {
				return err
			}
			secret.Data[caCertKey], secret.Data[caKeyKey] = caCert, caKey
			delete(secret.Data, corev1.TLSCertKey)
		}
		names := []string{cfg.DNSName()}
		if reason := pki.RenewalReason(secret.Data[corev1.TLSCertKey], secret.Data[caCertKey], names, now); reason != "" {
			logger.Info("Issuing webhook serving certificate", "reason", reason, "dnsName", cfg.DNSName())
			cert, key, err := pki.IssueLeaf(secret.Data[caCertKey], secret.Data[caKeyKey], names, now)
			if err != nil {
				return err
			}
			secret.Data[corev1.TLSCertKey], secret.Data[corev1.TLSPrivateKeyKey] = cert, key
		}
		secret.Type = corev1.SecretTypeOpaque
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("reconcile webhook certificate secret: %w", err)
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Webhook certificate secret reconciled", "operation", op)
	}
	if err := os.MkdirAll(cfg.CertDir, 0o700); err != nil {
		return nil, fmt.Errorf("create cert dir: %w", err)
	}
	// The manager's cert watcher reloads these when they change.
	for name, data := range map[string][]byte{corev1.TLSCertKey: secret.Data[corev1.TLSCertKey], corev1.TLSPrivateKeyKey: secret.Data[corev1.TLSPrivateKeyKey]} {
		if err := os.WriteFile(filepath.Join(cfg.CertDir, name), data, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", name, err)
		}
	}
	return secret.Data[caCertKey], nil
}

// EnsureConfiguration creates or updates the MutatingWebhookConfiguration.
func EnsureConfiguration(ctx context.Context, c client.Client, cfg Config, caPEM []byte) error {
	logger := log.FromContext(ctx)
	mwc := &admissionregistrationv1.MutatingWebhookConfiguration{ObjectMeta: metav1.ObjectMeta{Name: ConfigurationName}}
	op, err := controllerutil.CreateOrUpdate(ctx, c, mwc, func() error {
		mwc.Labels = map[string]string{"app.kubernetes.io/managed-by": "weft-operator"}
		path := Path
		port := int32(443)
		failure := admissionregistrationv1.Ignore
		sideEffects := admissionregistrationv1.SideEffectClassNoneOnDryRun
		timeout := int32(5)
		reinvoke := admissionregistrationv1.IfNeededReinvocationPolicy
		scope := admissionregistrationv1.NamespacedScope
		mwc.Webhooks = []admissionregistrationv1.MutatingWebhook{{
			Name: webhookName,
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service:  &admissionregistrationv1.ServiceReference{Namespace: cfg.Namespace, Name: cfg.ServiceName, Path: &path, Port: &port},
				CABundle: caPEM,
			},
			Rules: []admissionregistrationv1.RuleWithOperations{{
				Operations: []admissionregistrationv1.OperationType{admissionregistrationv1.Create},
				Rule: admissionregistrationv1.Rule{
					APIGroups: []string{""}, APIVersions: []string{"v1"}, Resources: []string{"pods"}, Scope: &scope,
				},
			}},
			NamespaceSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: "kubernetes.io/metadata.name", Operator: metav1.LabelSelectorOpNotIn, Values: cfg.ExcludedNamespaces,
			}}},
			ObjectSelector: &metav1.LabelSelector{MatchExpressions: []metav1.LabelSelectorRequirement{{
				Key: trust.OptOutKey, Operator: metav1.LabelSelectorOpNotIn, Values: []string{"false"},
			}}},
			FailurePolicy:           &failure,
			SideEffects:             &sideEffects,
			TimeoutSeconds:          &timeout,
			ReinvocationPolicy:      &reinvoke,
			AdmissionReviewVersions: []string{"v1"},
		}}
		return nil
	})
	if err != nil {
		return fmt.Errorf("reconcile %s: %w", ConfigurationName, err)
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Trust injection webhook configured", "configuration", ConfigurationName, "operation", op, "service", cfg.DNSName(), "excludedNamespaces", cfg.ExcludedNamespaces)
	}
	return nil
}

// Setup ensures the certificate and the webhook configuration. Run it once
// before the manager starts, then periodically via Renewer.
func Setup(ctx context.Context, c client.Client, cfg Config) error {
	caPEM, err := EnsureServingCert(ctx, c, cfg, time.Now())
	if err != nil {
		return err
	}
	return EnsureConfiguration(ctx, c, cfg, caPEM)
}

// Renewer is a manager Runnable that re-runs Setup daily so the serving
// certificate is renewed long before it expires.
type Renewer struct {
	Client client.Client
	Config Config
	Every  time.Duration
}

// Start implements manager.Runnable.
func (r *Renewer) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName("trust-webhook-renewer")
	every := r.Every
	if every == 0 {
		every = 24 * time.Hour
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := Setup(ctx, r.Client, r.Config); err != nil {
				logger.Error(err, "Renewing trust webhook certificate failed; will retry")
			}
		}
	}
}
