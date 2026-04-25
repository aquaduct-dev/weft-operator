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

package dnsrecord_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	"aquaduct.dev/weft-operator/internal/controller/aquaducttaas"
	"aquaduct.dev/weft-operator/internal/controller/dnsrecord"
)

// mockDomainClient records every call and replays whatever the test
// configured via set*Fn. Per-call hooks let a single test simulate a
// flapping backend (e.g. "GET succeeds, PUT fails once, then succeeds").
type mockDomainClient struct {
	mu       sync.Mutex
	GetFn    func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error)
	PutFn    func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error)
	PatchFn  func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error)
	DeleteFn func(ctx context.Context, token, name string) error
	LookupFn func(ctx context.Context, token, name string) ([]string, error)

	LastToken      string
	LastBastionIDs *[]string
	GetCalls       int
	PutCalls       int
	PatchCalls     int
	DelCalls       int
	LookCalls      int
}

func (m *mockDomainClient) GetDomain(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.GetCalls++
	if m.GetFn == nil {
		return nil, aquaducttaas.ErrDomainNotFound
	}
	return m.GetFn(ctx, token, name)
}

func (m *mockDomainClient) PutDomain(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.LastBastionIDs = bastionIDs
	m.PutCalls++
	if m.PutFn == nil {
		// Default: server accepts and "applies" whatever the caller
		// asked for. With bastionIDs nil, return a placeholder fan-out
		// of one bastion so tests that don't care about specific IDs
		// still get populated AppliedBastionIDs/IPs.
		applied := []string{"default-bastion"}
		ips := []string{"10.0.0.1"}
		if bastionIDs != nil {
			applied = *bastionIDs
			ips = ipsForBastions(applied)
		}
		return &aquaducttaas.Domain{
			ID:         "id-" + name,
			Name:       name,
			BastionIDs: applied,
			IPs:        ips,
		}, nil
	}
	return m.PutFn(ctx, token, name, bastionIDs)
}

func (m *mockDomainClient) PatchDomain(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.LastBastionIDs = bastionIDs
	m.PatchCalls++
	if m.PatchFn == nil {
		// Default: PATCH succeeds and "applies" the requested bastions.
		applied := []string{"default-bastion"}
		ips := []string{"10.0.0.1"}
		if bastionIDs != nil {
			applied = *bastionIDs
			ips = ipsForBastions(applied)
		}
		return &aquaducttaas.Domain{
			ID:         "id-" + name,
			Name:       name,
			BastionIDs: applied,
			IPs:        ips,
		}, nil
	}
	return m.PatchFn(ctx, token, name, bastionIDs)
}

// ipsForBastions returns a deterministic test IP per bastion ID so that
// mock responses and seeded BastionInfo lists agree. Real server-side
// resolution looks up each bastion's IP from the cluster's bastion
// inventory; for tests we just hash the ID into the last octet.
func ipsForBastions(ids []string) []string {
	out := make([]string, 0, len(ids))
	for i, id := range ids {
		_ = id
		out = append(out, fmt.Sprintf("10.0.0.%d", 10+i))
	}
	return out
}

func (m *mockDomainClient) DeleteDomain(ctx context.Context, token, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.DelCalls++
	if m.DeleteFn == nil {
		return nil
	}
	return m.DeleteFn(ctx, token, name)
}

func (m *mockDomainClient) LookupDomain(ctx context.Context, token, name string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.LookCalls++
	if m.LookupFn == nil {
		return []string{"10.0.0.1"}, nil
	}
	return m.LookupFn(ctx, token, name)
}

func (m *mockDomainClient) setGet(fn func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.GetFn = fn
}

func (m *mockDomainClient) setPut(fn func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PutFn = fn
}

func (m *mockDomainClient) setPatch(fn func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.PatchFn = fn
}

func (m *mockDomainClient) setDelete(fn func(ctx context.Context, token, name string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DeleteFn = fn
}

func (m *mockDomainClient) setLookup(fn func(ctx context.Context, token, name string) ([]string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LookupFn = fn
}

func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

var _ = Describe("DNSRecord Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	var (
		drName     string
		taasName   string
		secretName string
		domainName string
		mock       *mockDomainClient
	)

	BeforeEach(func() {
		suffix := randomSuffix()
		drName = "dr-" + suffix
		taasName = "taas-" + suffix
		secretName = "sec-" + suffix
		domainName = "host-" + suffix + ".example.com"
		mock = &mockDomainClient{}
	})

	newReconciler := func() *dnsrecord.DNSRecordReconciler {
		return &dnsrecord.DNSRecordReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIClient: mock,
		}
	}

	reconcile := func(ctx context.Context, r *dnsrecord.DNSRecordReconciler) (ctrl.Result, error) {
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: drName, Namespace: "default"}}
		var res ctrl.Result
		var err error
		// First reconcile on a fresh object only adds the finalizer;
		// loop until we're past the bootstrap hop so tests can assert
		// the steady-state outcome.
		for i := 0; i < 3; i++ {
			res, err = r.Reconcile(ctx, req)
			if err != nil || !res.Requeue {
				return res, err
			}
		}
		return res, err
	}

	createSecret := func(ctx context.Context, key, value string) {
		sec := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: "default"},
			Data:       map[string][]byte{key: []byte(value)},
		}
		Expect(k8sClient.Create(ctx, sec)).To(Succeed())
	}

	// createTaaS creates an AquaductTaaS and seeds its status with one
	// non-suspended bastion ("default-bastion" / 10.0.0.1) so the
	// DNSRecord reconciler can derive expectedIPs without depending on
	// a real AquaductTaaSReconciler running in envtest. Tests that need
	// a different bastion topology call seedTaaSBastions afterward.
	createTaaS := func(ctx context.Context, key string) *weftv1alpha1.AquaductTaaS {
		t := &weftv1alpha1.AquaductTaaS{
			ObjectMeta: metav1.ObjectMeta{Name: taasName, Namespace: "default"},
			Spec: weftv1alpha1.AquaductTaaSSpec{
				AccessTokenSecretRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  key,
				},
			},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		now := metav1.Now()
		t.Status.LastSyncTime = &now
		t.Status.Bastions = []weftv1alpha1.BastionInfo{
			{ID: "default-bastion", Name: "default", IP: "10.0.0.1"},
		}
		Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())
		return t
	}

	// seedTaaSBastions overrides the default bastion list on an
	// existing AquaductTaaS. Used by tests that need a specific
	// topology (multiple bastions, suspended, etc.).
	seedTaaSBastions := func(ctx context.Context, bastions []weftv1alpha1.BastionInfo) {
		t := &weftv1alpha1.AquaductTaaS{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taasName, Namespace: "default"}, t)).To(Succeed())
		now := metav1.Now()
		t.Status.LastSyncTime = &now
		t.Status.Bastions = bastions
		Expect(k8sClient.Status().Update(ctx, t)).To(Succeed())
	}
	_ = seedTaaSBastions // available for tests that need a non-default topology

	createDNSRecord := func(ctx context.Context) *weftv1alpha1.DNSRecord {
		d := &weftv1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{Name: drName, Namespace: "default"},
			Spec: weftv1alpha1.DNSRecordSpec{
				DomainName:      domainName,
				AquaductTaaSRef: corev1.LocalObjectReference{Name: taasName},
			},
		}
		Expect(k8sClient.Create(ctx, d)).To(Succeed())
		return d
	}

	getDNSRecord := func(ctx context.Context) *weftv1alpha1.DNSRecord {
		d := &weftv1alpha1.DNSRecord{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: drName, Namespace: "default"}, d)).To(Succeed())
		return d
	}

	// --- Happy paths -------------------------------------------------------

	It("Registers a new domain on first reconcile and records Ready=True", func(ctx context.Context) {
		createSecret(ctx, "token", "tok-new")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		res, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 30*time.Second),
			"successful sync uses the long requeue cadence")

		Expect(mock.GetCalls).To(BeNumerically(">=", 1))
		Expect(mock.PutCalls).To(Equal(1), "absent domain must be PUT exactly once")
		Expect(mock.LookCalls).To(BeNumerically(">=", 1))
		Expect(mock.LastToken).To(Equal("tok-new"))

		d := getDNSRecord(ctx)
		Expect(d.Status.DomainID).To(Equal("id-" + domainName))
		Expect(d.Status.ClobberedPreexisting).To(BeFalse(),
			"a record we created from scratch is not 'clobbered'")
		Expect(d.Status.ResolvedIPs).To(ConsistOf("10.0.0.1"))
		Expect(d.Status.ObservedGeneration).To(Equal(d.Generation))
		Expect(d.Status.LastSyncTime).NotTo(BeNil())

		reg := findCondition(d.Status.Conditions, "Registered")
		Expect(reg).NotTo(BeNil())
		Expect(reg.Status).To(Equal(metav1.ConditionTrue))
		res2 := findCondition(d.Status.Conditions, "Resolved")
		Expect(res2).NotTo(BeNil())
		Expect(res2.Status).To(Equal(metav1.ConditionTrue))
		ready := findCondition(d.Status.Conditions, "Ready")
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))
	})

	It("Clobbers a pre-existing registration via PATCH and marks ClobberedPreexisting=true", func(ctx context.Context) {
		createSecret(ctx, "token", "tok-clobber")
		createTaaS(ctx, "token")
		mock.setGet(func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
			return &aquaducttaas.Domain{ID: "preexisting-xyz", Name: name}, nil
		})
		mock.setPatch(func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
			// Server accepted the take-over; returns the (now ours) record.
			return &aquaducttaas.Domain{ID: "preexisting-xyz", Name: name}, nil
		})
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.PutCalls).To(Equal(0), "a pre-existing record is inherited, not re-created")
		Expect(mock.PatchCalls).To(Equal(1), "existing records must be PATCHed to assert ownership")

		d := getDNSRecord(ctx)
		Expect(d.Status.DomainID).To(Equal("preexisting-xyz"))
		Expect(d.Status.ClobberedPreexisting).To(BeTrue())
		Expect(findCondition(d.Status.Conditions, "Registered").Status).To(Equal(metav1.ConditionTrue))
	})

	It("Sets Registered=False with ForeignOwned when PATCH is refused AND DNS records are wrong", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		mock.setGet(func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
			return &aquaducttaas.Domain{ID: "someone-elses-id", Name: name}, nil
		})
		mock.setPatch(func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
			// The real HTTP client wraps a 403 as `%w: ...` around
			// ErrDomainForeign; the reconciler matches on errors.Is, so
			// returning the sentinel directly is equivalent for tests.
			return nil, aquaducttaas.ErrDomainForeign
		})
		// The foreign owner has the domain pointed at IPs that don't
		// match our expected bastion (default seed has 10.0.0.1).
		// That makes this a hard ForeignOwned failure rather than the
		// ExternallyManaged "happens to be correct" case.
		mock.setLookup(func(ctx context.Context, token, name string) ([]string, error) {
			return []string{"203.0.113.99"}, nil
		})
		createDNSRecord(ctx)

		res, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(30*time.Second),
			"a foreign-owned record should requeue on the error cadence so a later release of the domain heals automatically")

		Expect(mock.PatchCalls).To(Equal(1))
		Expect(mock.PutCalls).To(Equal(0), "we must not attempt PUT when the record belongs to someone else")

		d := getDNSRecord(ctx)
		cond := findCondition(d.Status.Conditions, "Registered")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("ForeignOwned"))
		Expect(cond.Message).To(ContainSubstring("different user"))

		// Ready follows Resolved, which is False (lookup mismatches
		// expected). The user-visible signal is "DNS isn't pointing
		// at our bastions" — the deeper "ForeignOwned" reason is in
		// the Registered condition for operators who care.
		ready := findCondition(d.Status.Conditions, "Ready")
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("Mismatched"))
	})

	It("Sets Registered=ExternallyManaged but Ready=True when foreign-owned record happens to be correct", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		mock.setGet(func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
			return &aquaducttaas.Domain{ID: "someone-elses-id", Name: name}, nil
		})
		mock.setPatch(func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
			return nil, aquaducttaas.ErrDomainForeign
		})
		// Lookup returns the same IP as our seeded bastion; the world
		// is "correct" via external configuration. Reconciliation
		// should report success even though we don't own the record.
		mock.setLookup(func(ctx context.Context, token, name string) ([]string, error) {
			return []string{"10.0.0.1"}, nil
		})
		createDNSRecord(ctx)

		res, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 30*time.Second),
			"once the world is correct (regardless of ownership), we requeue on the long cadence")

		d := getDNSRecord(ctx)
		reg := findCondition(d.Status.Conditions, "Registered")
		Expect(reg.Status).To(Equal(metav1.ConditionFalse))
		Expect(reg.Reason).To(Equal("ExternallyManaged"))
		Expect(reg.Message).To(ContainSubstring("match the expected bastion IPs"))

		ready := findCondition(d.Status.Conditions, "Ready")
		Expect(ready.Status).To(Equal(metav1.ConditionTrue),
			"DNS records correct => Ready=True even without ownership")
		Expect(ready.Reason).To(Equal("Ready"))
	})

	It("Recovers when PATCH races with an external deletion", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		// GET returns a record that vanishes before PATCH lands. Once
		// the PATCH observes the 404 and flips `vanished`, subsequent
		// GETs return 404 so the reconciler proceeds down the PUT path
		// within the same reconcile helper run. Observable outcome:
		// exactly one PUT was issued and Registered=True.
		vanished := int32(0)
		mock.setGet(func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
			if atomic.LoadInt32(&vanished) == 1 {
				return nil, aquaducttaas.ErrDomainNotFound
			}
			return &aquaducttaas.Domain{ID: "transient-id", Name: name}, nil
		})
		mock.setPatch(func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
			atomic.StoreInt32(&vanished, 1)
			return nil, aquaducttaas.ErrDomainNotFound
		})
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.PatchCalls).To(Equal(1),
			"the initial GET returned a record so PATCH was attempted exactly once")
		Expect(mock.PutCalls).To(Equal(1),
			"after the race resolves to 'missing', the reconciler falls through to PUT")

		d := getDNSRecord(ctx)
		Expect(findCondition(d.Status.Conditions, "Registered").Status).To(Equal(metav1.ConditionTrue))
	})

	It("Reports Resolved=Mismatched when DNS hasn't propagated yet", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		// Lookup returns no IPs — propagation lag is the typical case
		// just after a fresh PUT. We expect 10.0.0.1 (from the seeded
		// bastion) but DNS hasn't picked it up yet.
		mock.setLookup(func(ctx context.Context, token, name string) ([]string, error) {
			return nil, nil
		})
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		d := getDNSRecord(ctx)
		// Registered=True — the PUT succeeded; ownership is fine.
		Expect(findCondition(d.Status.Conditions, "Registered").Status).To(Equal(metav1.ConditionTrue))
		// Resolved=False reason=Mismatched — empty lookup vs non-empty
		// expected. The new semantics treat "no records" the same way
		// as "wrong records": both are differences from desired state.
		res := findCondition(d.Status.Conditions, "Resolved")
		Expect(res.Status).To(Equal(metav1.ConditionFalse))
		Expect(res.Reason).To(Equal("Mismatched"))
		Expect(findCondition(d.Status.Conditions, "Ready").Status).To(Equal(metav1.ConditionFalse))
	})

	It("Surfaces lookup transport failures as Resolved=LookupFailed while keeping Registered=True", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		mock.setLookup(func(ctx context.Context, token, name string) ([]string, error) {
			return nil, errors.New("resolver down")
		})
		createDNSRecord(ctx)

		res, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(30 * time.Second),
			"an isolated lookup failure must requeue on the short cadence")

		d := getDNSRecord(ctx)
		Expect(findCondition(d.Status.Conditions, "Registered").Status).To(Equal(metav1.ConditionTrue))
		resolved := findCondition(d.Status.Conditions, "Resolved")
		Expect(resolved.Status).To(Equal(metav1.ConditionFalse))
		Expect(resolved.Reason).To(Equal("LookupFailed"))
		Expect(resolved.Message).To(ContainSubstring("resolver down"))
	})

	// --- Error recovery ----------------------------------------------------

	It("Sets Registered=False with APIError when GET fails, and recovers on retry", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		failures := int32(0)
		mock.setGet(func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
			if atomic.AddInt32(&failures, 1) == 1 {
				return nil, errors.New("502 bad gateway")
			}
			return nil, aquaducttaas.ErrDomainNotFound // second call: safe to create
		})
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		By("first reconcile surfaces APIError")
		d := getDNSRecord(ctx)
		cond := findCondition(d.Status.Conditions, "Registered")
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("APIError"))
		Expect(cond.Message).To(ContainSubstring("502 bad gateway"))

		By("second reconcile succeeds and clears the error")
		_, err = reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		d = getDNSRecord(ctx)
		Expect(findCondition(d.Status.Conditions, "Registered").Status).To(Equal(metav1.ConditionTrue))
		Expect(findCondition(d.Status.Conditions, "Ready").Status).To(Equal(metav1.ConditionTrue))
	})

	It("Surfaces RegisterFailed when PUT fails", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		mock.setPut(func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
			return nil, errors.New("quota exceeded")
		})
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		cond := findCondition(getDNSRecord(ctx).Status.Conditions, "Registered")
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("RegisterFailed"))
		Expect(cond.Message).To(ContainSubstring("quota exceeded"))
	})

	It("Recovers when the server-side record is deleted between reconciles", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		By("first reconcile creates the record; we simulate a successful PUT")
		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		firstID := getDNSRecord(ctx).Status.DomainID
		Expect(firstID).NotTo(BeEmpty())

		By("the record is deleted server-side; GET now returns 404")
		mock.setGet(func(ctx context.Context, token, name string) (*aquaducttaas.Domain, error) {
			return nil, aquaducttaas.ErrDomainNotFound
		})
		// Next PUT should re-create it with a fresh ID, proving recovery.
		mock.setPut(func(ctx context.Context, token, name string, bastionIDs *[]string) (*aquaducttaas.Domain, error) {
			return &aquaducttaas.Domain{ID: "id-regenerated", Name: name}, nil
		})

		_, err = reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		d := getDNSRecord(ctx)
		Expect(d.Status.DomainID).To(Equal("id-regenerated"),
			"the reconciler must re-register and refresh DomainID, not keep the stale one")
		Expect(d.Status.ClobberedPreexisting).To(BeFalse(),
			"re-creating after a 404 is NOT a clobber — we just recovered our own record")
	})

	It("Sets ConfigError when APIClient is nil", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		r := &dnsrecord.DNSRecordReconciler{Client: k8sClient, Scheme: k8sClient.Scheme()}
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		cond := findCondition(getDNSRecord(ctx).Status.Conditions, "Registered")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("ConfigError"))
	})

	It("Sets MissingAquaductTaaS when the ref doesn't resolve", func(ctx context.Context) {
		createDNSRecord(ctx) // no AquaductTaaS / Secret created

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		cond := findCondition(getDNSRecord(ctx).Status.Conditions, "Registered")
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("MissingAquaductTaaS"))
		Expect(mock.GetCalls).To(Equal(0), "no API call should be attempted without credentials")
	})

	It("Sets SecretError when the Secret key is missing", func(ctx context.Context) {
		createSecret(ctx, "wrong-key", "tok")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		cond := findCondition(getDNSRecord(ctx).Status.Conditions, "Registered")
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("SecretError"))
	})

	// --- Finalizer & deletion --------------------------------------------

	It("Adds the unregister-on-delete finalizer on first reconcile", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		Expect(getDNSRecord(ctx).Finalizers).To(ContainElement("weft.aquaduct.dev/unregister-on-delete"))
	})

	It("Calls DeleteDomain and removes the finalizer on deletion", func(ctx context.Context) {
		createSecret(ctx, "token", "tok-del")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, getDNSRecord(ctx))).To(Succeed())

		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.DelCalls).To(Equal(1))
		Expect(mock.LastToken).To(Equal("tok-del"))

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: drName, Namespace: "default"}, &weftv1alpha1.DNSRecord{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue(), "the object is deleted once the finalizer is removed")
	})

	It("Treats a 404 on DeleteDomain as success (record already gone)", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		// HTTPAPIClient.DeleteDomain collapses 404 into nil; the mock
		// must behave the same way for this test to exercise the code
		// path that relies on that invariant.
		mock.setDelete(func(ctx context.Context, token, name string) error { return nil })

		Expect(k8sClient.Delete(ctx, getDNSRecord(ctx))).To(Succeed())
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: drName, Namespace: "default"}, &weftv1alpha1.DNSRecord{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("Blocks deletion and retries when DeleteDomain fails", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		createDNSRecord(ctx)

		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		mock.setDelete(func(ctx context.Context, token, name string) error {
			return errors.New("cloud down")
		})

		Expect(k8sClient.Delete(ctx, getDNSRecord(ctx))).To(Succeed())
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		d := getDNSRecord(ctx)
		Expect(d.Finalizers).To(ContainElement("weft.aquaduct.dev/unregister-on-delete"),
			"the finalizer must stay until DeleteDomain succeeds")
		ready := findCondition(d.Status.Conditions, "Ready")
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Reason).To(Equal("CleanupFailed"))

		By("Once the backend recovers, the next reconcile completes cleanup")
		mock.setDelete(nil)
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: drName, Namespace: "default"}, &weftv1alpha1.DNSRecord{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("Blocks deletion when AquaductTaaS has been removed first", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		taas := createTaaS(ctx, "token")
		createDNSRecord(ctx)

		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		By("Deleting the AquaductTaaS out from under the DNSRecord")
		// Drop the AquaductTaaS finalizer first so it's not blocked by
		// its own suspend-on-delete logic — we're simulating a cluster
		// where someone force-removed the credentials CR.
		taas.Finalizers = nil
		Expect(k8sClient.Update(ctx, taas)).To(Succeed())
		Expect(k8sClient.Delete(ctx, taas)).To(Succeed())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taasName, Namespace: "default"}, &weftv1alpha1.AquaductTaaS{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())

		Expect(k8sClient.Delete(ctx, getDNSRecord(ctx))).To(Succeed())
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		d := getDNSRecord(ctx)
		Expect(d.Finalizers).To(ContainElement("weft.aquaduct.dev/unregister-on-delete"),
			"without credentials we can't reach the API — the finalizer must stay")
		cond := findCondition(d.Status.Conditions, "Registered")
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("MissingAquaductTaaS"))
	})

	It("Is a no-op for a DNSRecord that's already been deleted", func(ctx context.Context) {
		// Request a reconcile for a name that was never created.
		r := newReconciler()
		res, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "missing-" + randomSuffix(), Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(mock.GetCalls).To(Equal(0))
	})

	// --- API server validation -------------------------------------------

	It("Rejects mutations to spec.domainName via the CEL immutability rule", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, "token")
		dr := createDNSRecord(ctx)

		dr.Spec.DomainName = "another.example.com"
		err := k8sClient.Update(ctx, dr)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("immutable"),
			"the API server should reject the update with the CEL rule's message")
	})
})

// randomSuffix returns a process-unique string so objects created in the
// shared envtest cluster don't collide between It blocks.
func randomSuffix() string {
	return fmt.Sprintf("%d", atomic.AddInt64(&suffixCounter, 1))
}

var suffixCounter int64
