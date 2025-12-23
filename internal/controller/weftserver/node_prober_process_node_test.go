package weftserver

import (
	"context"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	ctrlfake "sigs.k8s.io/controller-runtime/pkg/client/fake"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
)

func TestProcessNode_AutoscalerNode(t *testing.T) {
	RegisterTestingT(t)

	scheme := runtime.NewScheme()
	corev1.AddToScheme(scheme)
	weftv1alpha1.AddToScheme(scheme)

	// Create a fake client-go clientset for the NodeProber
	clientset := fake.NewSimpleClientset()

	// Create a fake controller-runtime client
	cl := ctrlfake.NewClientBuilder().WithScheme(scheme).Build()

	prober := &NodeProber{
		Client:    cl,
		Scheme:    scheme,
		Clientset: clientset,
	}

	autoscalerNode := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-autoscaler-node",
			Labels: map[string]string{
				"node.kubernetes.io/role": "autoscaler-node",
			},
		},
	}

	err := prober.processNode(context.Background(), autoscalerNode)
	Expect(err).NotTo(HaveOccurred())

	// Verify no WeftServer is created for the autoscaler node
	var wsList weftv1alpha1.WeftServerList
	err = cl.List(context.Background(), &wsList)
	Expect(err).NotTo(HaveOccurred())
	Expect(wsList.Items).To(BeEmpty(), "No WeftServer should be created for an autoscaler node")
}

// TODO: Add a test for a non-autoscaler node to ensure WeftServer is created.
// This will require properly mocking the probe job creation, execution, and log retrieval to simulate a successful probe.
