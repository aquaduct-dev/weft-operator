package weftserver

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

func TestCleanupOrphanedServers(t *testing.T) {
	RegisterTestingT(t)

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	weftv1alpha1.AddToScheme(scheme)

	// Create a fake client
	// Scenario:
	// - Node "exists" exists
	// - WeftServer "ws-exists" references "exists"
	// - WeftServer "ws-missing" references "missing"
	// - WeftServer "ws-no-label" has no label

	nodeExists := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "exists",
		},
	}

	wsExists := &weftv1alpha1.WeftServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws-exists",
			Namespace: "default",
			Labels: map[string]string{
				"weft.aquaduct.dev/node": "exists",
			},
		},
	}

	wsMissing := &weftv1alpha1.WeftServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws-missing",
			Namespace: "default",
			Labels: map[string]string{
				"weft.aquaduct.dev/node": "missing",
			},
		},
	}

	wsNoLabel := &weftv1alpha1.WeftServer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ws-no-label",
			Namespace: "default",
		},
	}

	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(nodeExists, wsExists, wsMissing, wsNoLabel).Build()

	prober := &NodeProber{
		Client: cl,
	}

	err := prober.cleanupOrphanedServers(context.Background())
	Expect(err).NotTo(HaveOccurred())

	// Verify ws-exists still exists
	err = cl.Get(context.Background(), client.ObjectKeyFromObject(wsExists), &weftv1alpha1.WeftServer{})
	Expect(err).NotTo(HaveOccurred())

	// Verify ws-missing is deleted
	err = cl.Get(context.Background(), client.ObjectKeyFromObject(wsMissing), &weftv1alpha1.WeftServer{})
	Expect(err).To(HaveOccurred()) // Should be NotFound

	// Verify ws-no-label still exists
	err = cl.Get(context.Background(), client.ObjectKeyFromObject(wsNoLabel), &weftv1alpha1.WeftServer{})
	Expect(err).NotTo(HaveOccurred())
}
