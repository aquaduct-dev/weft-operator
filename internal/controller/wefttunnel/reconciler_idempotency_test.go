package wefttunnel_test

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	weftv1alpha1 "aquaduct.dev/weft-operator/api/v1alpha1"
	wefttunnel "aquaduct.dev/weft-operator/internal/controller/wefttunnel"
)

// countingClient tallies Update calls against Deployments so a test can prove
// a converged reconcile stops writing. Status subresource writes go through
// Status() and are deliberately not counted.
type countingClient struct {
	client.Client
	deploymentUpdates int
}

func (c *countingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if _, ok := obj.(*appsv1.Deployment); ok {
		c.deploymentUpdates++
	}
	return c.Client.Update(ctx, obj, opts...)
}

var _ = Describe("WeftTunnel Deployment convergence", func() {
	It("Should stop writing the Deployment once it matches desired state", func(ctx context.Context) {
		By("Creating a WeftServer and a WeftTunnel")
		server := &weftv1alpha1.WeftServer{
			ObjectMeta: metav1.ObjectMeta{Name: "converge-server", Namespace: "default"},
			Spec: weftv1alpha1.WeftServerSpec{
				Location:         weftv1alpha1.WeftServerLocationExternal,
				ConnectionString: "weft://secret@10.0.0.1:9092",
			},
		}
		Expect(k8sClient.Create(ctx, server)).To(Succeed())

		tunnel := &weftv1alpha1.WeftTunnel{
			ObjectMeta: metav1.ObjectMeta{Name: "converge-tunnel", Namespace: "default"},
			Spec: weftv1alpha1.WeftTunnelSpec{
				TargetServers: []string{server.Name},
				SrcURL:        "http://src",
				DstURL:        "https://dst",
			},
		}
		Expect(k8sClient.Create(ctx, tunnel)).To(Succeed())

		counting := &countingClient{Client: k8sClient}
		r := &wefttunnel.WeftTunnelReconciler{Client: counting, Scheme: k8sClient.Scheme()}
		req := ctrl.Request{NamespacedName: types.NamespacedName{Name: tunnel.Name, Namespace: tunnel.Namespace}}

		By("Reconciling once to create the Deployment")
		_, err := r.Reconcile(ctx, req)
		Expect(err).NotTo(HaveOccurred())

		depName := fmt.Sprintf("tunnel-%s-to-%s", tunnel.Name, server.Name)
		dep := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: depName, Namespace: "default"}, dep)).To(Succeed())

		// The API server defaults fields the reconciler never sets. If the
		// mutate function clobbers them it will diff against the stored
		// object forever, updating on every pass.
		Expect(dep.Spec.Template.Spec.Containers[0].TerminationMessagePath).NotTo(BeEmpty())

		By("Reconciling twice more against the now-defaulted Deployment")
		counting.deploymentUpdates = 0
		for i := 0; i < 2; i++ {
			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		}

		Expect(counting.deploymentUpdates).To(BeZero(),
			"a converged reconcile must not write the Deployment; repeated writes fed the "+
				"watch loop that starved the operator")
	})
})
