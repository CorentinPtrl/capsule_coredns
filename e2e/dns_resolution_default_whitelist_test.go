// Copyright 2025-2026 PITREL Corentin
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("DNS resolution from tenant namespace to whitelisted namespace (default)", Label("dns"), func() {
	var (
		nsName  = "tenant-dns-test-ns"
		podName = "dns-test-pod"
	)

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-dns-test",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "jack",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tnt.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tnt)
		}).Should(Succeed())
		By("creating the Namespace", func() {
			ns := NewNamespace(nsName)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		})
	})
	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		By("deleting the Namespace", func() {
			ns := NewNamespace(nsName)
			err := k8sClient.Delete(context.TODO(), ns)
			if err != nil && !apierrors.IsNotFound(err) {
				Expect(err).ToNot(HaveOccurred())
			}
		})
	})

	It("should resolve kubernetes.default.svc.cluster.local from a tenant pod", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)
		By("deploying a busybox pod")
		pod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: nsName,
				Labels:    map[string]string{"app": "dns-test"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:    "busybox",
					Image:   "busybox",
					Command: []string{"sleep", "3600"},
				}},
				RestartPolicy: corev1.RestartPolicyNever,
			},
		}
		_, err := cs.CoreV1().Pods(nsName).Create(context.TODO(), pod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the pod to be running")
		Eventually(func() corev1.PodPhase {
			p, _ := cs.CoreV1().Pods(nsName).Get(context.TODO(), podName, metav1.GetOptions{})
			return p.Status.Phase
		}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

		By("executing nslookup for kubernetes.default.svc.cluster.local")
		cmd := []string{"nslookup", "kubernetes.default.svc.cluster.local"}
		stdout, stderr, err := ExecInPod(cs, nsName, podName, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdout).To(ContainSubstring("Name:\tkubernetes.default.svc.cluster.local"))
		Expect(stdout).To(MatchRegexp(`Address: [0-9.]+`))
		By("deleting the busybox pod")
		Expect(cs.CoreV1().Pods(nsName).Delete(context.TODO(), podName, metav1.DeleteOptions{})).Should(Succeed())
		Eventually(func() bool {
			_, err := cs.CoreV1().Pods(nsName).Get(context.TODO(), podName, metav1.GetOptions{})
			return apierrors.IsNotFound(err)
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
