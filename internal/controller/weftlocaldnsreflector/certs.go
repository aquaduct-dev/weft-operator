// certs.go — the self-signed CA and serving certificate a WeftLocalDNSReflector
// terminates TLS with. The CA is generated once and kept in a Secret; the
// serving certificate is reissued from it whenever its SANs drift from the
// spec hostnames, it no longer chains to the CA, or it is close to expiry.
// The X.509 work lives in internal/pki; this file only manages the Secrets.
// Key material never leaves the Secrets and is never logged.
package weftlocaldnsreflector

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/pki"
)

// issuedCertificates is what ensureCertificates hands the rest of Reconcile.
type issuedCertificates struct {
	CACertPEM   []byte
	Fingerprint string
	NotAfter    time.Time
}

// ensureCertificates makes sure the CA Secret exists and the serving Secret
// holds a current certificate for hostnames. Both Secrets are owned by the
// reflector so deleting it garbage-collects them.
func (r *Reconciler) ensureCertificates(ctx context.Context, refl *weftv1alpha1.WeftLocalDNSReflector, hostnames []string) (issuedCertificates, error) {
	logger := log.FromContext(ctx)
	ns := refl.Spec.Namespace
	now := r.now()

	caSecret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Namespace: ns, Name: caSecretName(refl)}, caSecret)
	switch {
	case apierrors.IsNotFound(err):
		logger.Info("Generating reflector CA", "secret", ns+"/"+caSecretName(refl))
		certPEM, keyPEM, genErr := pki.NewCA("weft-local-dns-reflector "+refl.Name, now)
		if genErr != nil {
			return issuedCertificates{}, genErr
		}
		caSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: caSecretName(refl), Labels: managedLabels(refl, "ca")},
			Type:       corev1.SecretTypeTLS,
			Data:       map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM},
		}
		if err := controllerutil.SetOwnerReference(refl, caSecret, r.Scheme); err != nil {
			return issuedCertificates{}, err
		}
		if err := r.Create(ctx, caSecret); err != nil {
			return issuedCertificates{}, fmt.Errorf("create CA secret: %w", err)
		}
	case err != nil:
		return issuedCertificates{}, fmt.Errorf("get CA secret: %w", err)
	}
	caCertPEM := caSecret.Data[corev1.TLSCertKey]
	caKeyPEM := caSecret.Data[corev1.TLSPrivateKeyKey]

	tlsSecret := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: tlsSecretName(refl)}}
	var leafPEM []byte
	op, err := controllerutil.CreateOrUpdate(ctx, r.Client, tlsSecret, func() error {
		tlsSecret.Type = corev1.SecretTypeTLS
		tlsSecret.Labels = managedLabels(refl, "serving")
		if reason := pki.RenewalReason(tlsSecret.Data[corev1.TLSCertKey], caCertPEM, hostnames, now); reason != "" {
			logger.Info("Issuing serving certificate", "secret", ns+"/"+tlsSecretName(refl), "reason", reason, "hostnames", hostnames)
			certPEM, keyPEM, err := pki.IssueLeaf(caCertPEM, caKeyPEM, hostnames, now)
			if err != nil {
				return err
			}
			tlsSecret.Data = map[string][]byte{corev1.TLSCertKey: certPEM, corev1.TLSPrivateKeyKey: keyPEM}
		}
		leafPEM = tlsSecret.Data[corev1.TLSCertKey]
		return controllerutil.SetOwnerReference(refl, tlsSecret, r.Scheme)
	})
	if err != nil {
		return issuedCertificates{}, fmt.Errorf("reconcile serving secret: %w", err)
	}
	if op != controllerutil.OperationResultNone {
		logger.Info("Serving certificate secret reconciled", "operation", op)
	}

	leaf, err := pki.ParseCert(leafPEM)
	if err != nil {
		return issuedCertificates{}, fmt.Errorf("parse serving certificate: %w", err)
	}
	return issuedCertificates{CACertPEM: caCertPEM, Fingerprint: pki.Fingerprint(leafPEM), NotAfter: leaf.NotAfter}, nil
}
