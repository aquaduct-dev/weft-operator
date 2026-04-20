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

package aquaducttaas_test

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
)

// mockAPIClient is a test-only APIClient that replays whatever ListFn returns.
// Callers can rewrite ListFn / SuspendFn mid-test to simulate the cloud state
// changing between reconciles.
type mockAPIClient struct {
	mu        sync.Mutex
	ListFn    func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error)
	SuspendFn func(ctx context.Context, token, name string) error

	// LastToken captures the token passed on the most recent call so tests can
	// assert the reconciler actually reads and forwards the Secret.
	LastToken string
	Calls     int

	// Suspends records every server name SuspendServer was called with, in
	// order. Tests assert against this to verify deletion cleanup.
	Suspends []string
}

func (m *mockAPIClient) ListExternalServers(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.Calls++
	if m.ListFn == nil {
		return nil, nil
	}
	return m.ListFn(ctx, token)
}

func (m *mockAPIClient) SuspendServer(ctx context.Context, token, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.LastToken = token
	m.Suspends = append(m.Suspends, name)
	if m.SuspendFn == nil {
		return nil
	}
	return m.SuspendFn(ctx, token, name)
}

func (m *mockAPIClient) setList(fn func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ListFn = fn
}

func (m *mockAPIClient) setSuspend(fn func(ctx context.Context, token, name string) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.SuspendFn = fn
}

// findCondition returns the Available condition on the object, or nil.
func findCondition(conds []metav1.Condition, t string) *metav1.Condition {
	for i := range conds {
		if conds[i].Type == t {
			return &conds[i]
		}
	}
	return nil
}

var _ = Describe("AquaductTaaS Controller", func() {
	const (
		timeout  = 10 * time.Second
		interval = 250 * time.Millisecond
	)

	// Each It uses a unique object name so tests don't collide in the shared
	// envtest namespace. No cleanup is necessary because envtest resets
	// between test runs, and owner-ref GC handles child objects.
	var (
		taasName   string
		secretName string
		mock       *mockAPIClient
	)

	BeforeEach(func() {
		taasName = "taas-" + randomSuffix()
		secretName = taasName + "-token"
		mock = &mockAPIClient{}
	})

	newReconciler := func() *aquaducttaas.AquaductTaaSReconciler {
		return &aquaducttaas.AquaductTaaSReconciler{
			Client:    k8sClient,
			Scheme:    k8sClient.Scheme(),
			APIClient: mock,
		}
	}

	// reconcile drives the object to a steady state in one call. The first
	// Reconcile on a new object only adds the finalizer and returns
	// Requeue=true; by looping while Requeue=true we let tests assert against
	// the post-finalizer behavior without caring about the bootstrap hop.
	reconcile := func(ctx context.Context, r *aquaducttaas.AquaductTaaSReconciler) (ctrl.Result, error) {
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: taasName, Namespace: "default"}}
		var res ctrl.Result
		var err error
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

	createTaaS := func(ctx context.Context, ref *corev1.SecretKeySelector) *weftv1alpha1.AquaductTaaS {
		t := &weftv1alpha1.AquaductTaaS{
			ObjectMeta: metav1.ObjectMeta{Name: taasName, Namespace: "default"},
			Spec:       weftv1alpha1.AquaductTaaSSpec{AccessTokenSecretRef: ref},
		}
		Expect(k8sClient.Create(ctx, t)).To(Succeed())
		return t
	}

	getTaaS := func(ctx context.Context) *weftv1alpha1.AquaductTaaS {
		t := &weftv1alpha1.AquaductTaaS{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: taasName, Namespace: "default"}, t)).To(Succeed())
		return t
	}

	It("Sets Available=False when spec.accessTokenSecretRef is unset", func(ctx context.Context) {
		createTaaS(ctx, nil)

		res, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 0))

		t := getTaaS(ctx)
		cond := findCondition(t.Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("SpecInvalid"))

		Expect(mock.Calls).To(Equal(0), "API should not be called without a token ref")
	})

	It("Sets Available=False when the referenced Secret is missing", func(ctx context.Context) {
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		t := getTaaS(ctx)
		cond := findCondition(t.Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("SecretError"))
		Expect(cond.Message).To(ContainSubstring("not found"))
		Expect(mock.Calls).To(Equal(0))
	})

	It("Sets Available=False when the Secret lacks the referenced key", func(ctx context.Context) {
		createSecret(ctx, "wrong-key", "abc")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		cond := findCondition(getTaaS(ctx).Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("SecretError"))
		Expect(cond.Message).To(ContainSubstring("token"))
		Expect(mock.Calls).To(Equal(0))
	})

	It("Sets Available=False when the API call fails", func(ctx context.Context) {
		createSecret(ctx, "token", "super-secret")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return nil, errors.New("boom")
		})

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		cond := findCondition(getTaaS(ctx).Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("APIError"))
		Expect(cond.Message).To(ContainSubstring("boom"))
		Expect(mock.LastToken).To(Equal("super-secret"), "reconciler must forward the Secret data to the API client")
	})

	It("Creates WeftServers with Location=External from the API response", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		taas := createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		srvName := "cloud-bastion-" + randomSuffix()
		srvID := "id-" + srvName
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: srvID, Name: srvName, ConnectionString: "weft://secret1@1.2.3.4:8080"},
			}, nil
		})

		res, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(BeNumerically(">", 30*time.Second),
			"a successful sync should requeue on the long interval, not the error retry")

		ws := &weftv1alpha1.WeftServer{}
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: srvName, Namespace: "default"}, ws)
		}, timeout, interval).Should(Succeed())

		Expect(ws.Spec.Location).To(Equal(weftv1alpha1.WeftServerLocationExternal))
		Expect(ws.Spec.ConnectionString).To(Equal("weft://secret1@1.2.3.4:8080"))
		Expect(ws.Labels).To(HaveKeyWithValue("weft.aquaduct.dev/aquaducttaas", taas.Name))
		Expect(ws.Annotations).To(HaveKeyWithValue("weft.aquaduct.dev/bastion-id", srvID),
			"the bastion ID must be stamped on the WeftServer so the deletion path can look it up for PATCH /bastion/{id}")

		By("owner reference wiring the child to the AquaductTaaS for GC")
		Expect(ws.OwnerReferences).To(HaveLen(1))
		Expect(ws.OwnerReferences[0].Name).To(Equal(taas.Name))
		Expect(ws.OwnerReferences[0].Kind).To(Equal("AquaductTaaS"))
		Expect(ws.OwnerReferences[0].Controller).To(Not(BeNil()))
		Expect(*ws.OwnerReferences[0].Controller).To(BeTrue())

		By("recording Available=True plus the sync metadata")
		t := getTaaS(ctx)
		cond := findCondition(t.Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionTrue))
		Expect(cond.Reason).To(Equal("Synced"))
		Expect(t.Status.SyncedServers).To(ConsistOf(srvName))
		Expect(t.Status.LastSyncTime).NotTo(BeNil())
	})

	It("Updates an existing WeftServer when its ConnectionString changes", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		srvName := "cloud-bastion-" + randomSuffix()

		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: "id-" + srvName, Name: srvName, ConnectionString: "weft://v1@1.1.1.1:8080"},
			}, nil
		})
		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: "id-" + srvName, Name: srvName, ConnectionString: "weft://v2@2.2.2.2:9000"},
			}, nil
		})
		_, err = reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		ws := &weftv1alpha1.WeftServer{}
		Eventually(func() string {
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: srvName, Namespace: "default"}, ws); err != nil {
				return ""
			}
			return ws.Spec.ConnectionString
		}, timeout, interval).Should(Equal("weft://v2@2.2.2.2:9000"))
	})

	It("Prunes WeftServers that aquaduct.dev no longer returns", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})

		keptName := "kept-" + randomSuffix()
		goneName := "gone-" + randomSuffix()

		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: "id-kept", Name: keptName, ConnectionString: "weft://k@1.1.1.1:80"},
				{ID: "id-gone", Name: goneName, ConnectionString: "weft://g@2.2.2.2:80"},
			}, nil
		})
		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		By("Both servers should exist after the first sync")
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: keptName, Namespace: "default"}, &weftv1alpha1.WeftServer{})
		}, timeout, interval).Should(Succeed())
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: goneName, Namespace: "default"}, &weftv1alpha1.WeftServer{})
		}, timeout, interval).Should(Succeed())

		By("After the API drops goneName, the reconciler deletes it")
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: "id-kept", Name: keptName, ConnectionString: "weft://k@1.1.1.1:80"},
			}, nil
		})
		_, err = reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: goneName, Namespace: "default"}, &weftv1alpha1.WeftServer{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())

		By("keptName is still there")
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: keptName, Namespace: "default"}, &weftv1alpha1.WeftServer{})).
			To(Succeed())

		By("SyncedServers status reflects only the remaining server")
		Expect(getTaaS(ctx).Status.SyncedServers).To(ConsistOf(keptName))
	})

	It("Does not prune WeftServers that belong to a different AquaductTaaS", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})

		foreignName := "foreign-" + randomSuffix()
		foreign := &weftv1alpha1.WeftServer{
			ObjectMeta: metav1.ObjectMeta{
				Name:      foreignName,
				Namespace: "default",
				Labels:    map[string]string{"weft.aquaduct.dev/aquaducttaas": "somebody-else"},
			},
			Spec: weftv1alpha1.WeftServerSpec{
				Location:         weftv1alpha1.WeftServerLocationExternal,
				ConnectionString: "weft://foreign@3.3.3.3:80",
			},
		}
		Expect(k8sClient.Create(ctx, foreign)).To(Succeed())

		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return nil, nil // we own nothing
		})
		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: foreignName, Namespace: "default"}, &weftv1alpha1.WeftServer{})).
			To(Succeed(), "a WeftServer owned by a different AquaductTaaS must not be pruned")
	})

	It("Treats duplicate server names from the API as a SyncError", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: "a", Name: "dup", ConnectionString: "weft://a@1.1.1.1:80"},
				{ID: "b", Name: "dup", ConnectionString: "weft://b@2.2.2.2:80"},
			}, nil
		})
		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())
		cond := findCondition(getTaaS(ctx).Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("SyncError"))
	})

	It("Is a no-op when the AquaductTaaS has been deleted", func(ctx context.Context) {
		// Do not create the object — request a reconcile for a missing name.
		r := newReconciler()
		res, err := r.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{Name: "missing-" + randomSuffix(), Namespace: "default"},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(ctrl.Result{}))
		Expect(mock.Calls).To(Equal(0))
	})

	It("Adds the suspend-on-delete finalizer on first reconcile", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return nil, nil
		})

		_, err := reconcile(ctx, newReconciler())
		Expect(err).NotTo(HaveOccurred())

		Expect(getTaaS(ctx).Finalizers).To(ContainElement("weft.aquaduct.dev/suspend-on-delete"))
	})

	It("Suspends every managed bastion on delete and drops the finalizer", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		s1 := "bastion-a-" + randomSuffix()
		s2 := "bastion-b-" + randomSuffix()
		id1 := "id-a-" + randomSuffix()
		id2 := "id-b-" + randomSuffix()
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{
				{ID: id1, Name: s1, ConnectionString: "weft://a@1.1.1.1:80"},
				{ID: id2, Name: s2, ConnectionString: "weft://b@2.2.2.2:80"},
			}, nil
		})

		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		By("Deleting the AquaductTaaS — this sets DeletionTimestamp but the finalizer holds the object")
		Expect(k8sClient.Delete(ctx, getTaaS(ctx))).To(Succeed())

		By("Reconciling while marked-for-delete — suspends and removes finalizer")
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.Suspends).To(ConsistOf(id1, id2),
			"SuspendServer must be called with the bastion ID (not the k8s name), once per managed bastion")
		Expect(mock.LastToken).To(Equal("tok"))

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taasName, Namespace: "default"}, &weftv1alpha1.AquaductTaaS{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue(), "apiserver should delete the object once the last finalizer is gone")
	})

	It("Blocks deletion and retries when SuspendServer fails", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		srv := "bastion-" + randomSuffix()
		srvID := "id-" + srv
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return []aquaducttaas.ExternalServer{{ID: srvID, Name: srv, ConnectionString: "weft://x@1.1.1.1:80"}}, nil
		})
		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		mock.setSuspend(func(ctx context.Context, token, name string) error {
			return errors.New("cloud down")
		})

		Expect(k8sClient.Delete(ctx, getTaaS(ctx))).To(Succeed())
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		t := getTaaS(ctx) // must still exist — finalizer not removed
		Expect(t.Finalizers).To(ContainElement("weft.aquaduct.dev/suspend-on-delete"))
		cond := findCondition(t.Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("SuspendError"))
		Expect(cond.Message).To(ContainSubstring("cloud down"))

		By("Once the cloud recovers, the next reconcile completes cleanup")
		mock.setSuspend(nil)
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taasName, Namespace: "default"}, &weftv1alpha1.AquaductTaaS{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("On delete with no managed bastions, just drops the finalizer", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		mock.setList(func(ctx context.Context, token string) ([]aquaducttaas.ExternalServer, error) {
			return nil, nil
		})
		r := newReconciler()
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		Expect(k8sClient.Delete(ctx, getTaaS(ctx))).To(Succeed())
		_, err = reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())

		Expect(mock.Suspends).To(BeEmpty())
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: taasName, Namespace: "default"}, &weftv1alpha1.AquaductTaaS{})
			return k8serrors.IsNotFound(err)
		}, timeout, interval).Should(BeTrue())
	})

	It("Fails with ConfigError when APIClient is nil", func(ctx context.Context) {
		createSecret(ctx, "token", "tok")
		createTaaS(ctx, &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
			Key:                  "token",
		})
		r := &aquaducttaas.AquaductTaaSReconciler{
			Client: k8sClient,
			Scheme: k8sClient.Scheme(),
			// APIClient intentionally nil
		}
		_, err := reconcile(ctx, r)
		Expect(err).NotTo(HaveOccurred())
		cond := findCondition(getTaaS(ctx).Status.Conditions, "Available")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Reason).To(Equal("ConfigError"))
	})
})

// randomSuffix returns a string that is unique across a single test run so
// objects created in the shared envtest cluster don't collide between It blocks.
func randomSuffix() string {
	return fmt.Sprintf("%d", atomic.AddInt64(&suffixCounter, 1))
}

var suffixCounter int64
