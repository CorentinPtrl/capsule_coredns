// Copyright 2025-2026 PITREL Corentin
// SPDX-License-Identifier: Apache-2.0

package e2e

import (
	"context"
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("DNS resolution with service label whitelisting", Label("dns", "service-labels"), func() {
	var (
		tenantANs = "tenant-svclabel-a-ns"
		tenantBNs = "tenant-svclabel-b-ns"
		podName   = "dns-test-pod"
		svcName   = "labeled-service"
	)

	tenantA := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svclabel-tenant-a",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "owner-svc-a",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	tenantB := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "svclabel-tenant-b",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "owner-svc-b",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	JustBeforeEach(func() {
		EventuallyCreation(func() error {
			tenantA.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tenantA)
		}).Should(Succeed())

		EventuallyCreation(func() error {
			tenantB.ResourceVersion = ""
			return k8sClient.Create(context.TODO(), tenantB)
		}).Should(Succeed())

		By("creating namespace for tenant A", func() {
			ns := NewNamespace(tenantANs)
			NamespaceCreation(ns, tenantA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tenantA, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		})

		By("creating namespace for tenant B", func() {
			ns := NewNamespace(tenantBNs)
			NamespaceCreation(ns, tenantB.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tenantB, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		})
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tenantA)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), tenantB)).Should(Succeed())
		By("deleting namespaces", func() {
			for _, nsName := range []string{tenantANs, tenantBNs} {
				ns := NewNamespace(nsName)
				err := k8sClient.Delete(context.TODO(), ns)
				if err != nil && !apierrors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})
	})

	It("should allow a pod in tenant A to resolve a labeled service in tenant B when service labels are configured", func() {
		csA := ownerClient(tenantA.Spec.Owners[0].UserSpec)
		csB := ownerClient(tenantB.Spec.Owners[0].UserSpec)

		By("deploying a service with the expose-dns label in tenant B's namespace")
		backendPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "labeled-backend",
				Namespace: tenantBNs,
				Labels:    map[string]string{"app": "labeled-backend"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{{
					Name:  "nginx",
					Image: "nginx:alpine",
					Ports: []corev1.ContainerPort{{ContainerPort: 80}},
				}},
				RestartPolicy: corev1.RestartPolicyNever,
			},
		}
		_, err := csB.CoreV1().Pods(tenantBNs).Create(context.TODO(), backendPod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: tenantBNs,
				Labels: map[string]string{
					"capsule.io/expose-dns": "true",
				},
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "labeled-backend"},
				Ports: []corev1.ServicePort{{
					Port:       80,
					TargetPort: intstr.FromInt32(80),
				}},
			},
		}
		_, err = csB.CoreV1().Services(tenantBNs).Create(context.TODO(), svc, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("deploying a client pod in tenant A's namespace")
		clientPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: tenantANs,
				Labels:    map[string]string{"app": "dns-client"},
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
		_, err = csA.CoreV1().Pods(tenantANs).Create(context.TODO(), clientPod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the client pod to be running")
		Eventually(func() corev1.PodPhase {
			p, _ := csA.CoreV1().Pods(tenantANs).Get(context.TODO(), podName, metav1.GetOptions{})
			return p.Status.Phase
		}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

		By("executing nslookup for the labeled service in tenant B - should succeed due to service label whitelisting")
		serviceFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, tenantBNs)
		cmd := []string{"nslookup", serviceFQDN}
		stdout, stderr, err := ExecInPod(csA, tenantANs, podName, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdout).To(ContainSubstring(fmt.Sprintf("Name:\t%s", serviceFQDN)))
		Expect(stdout).To(MatchRegexp(`Address: [0-9.]+`))

		By("cleaning up")
		Expect(csA.CoreV1().Pods(tenantANs).Delete(context.TODO(), podName, metav1.DeleteOptions{})).Should(Succeed())
		Expect(csB.CoreV1().Pods(tenantBNs).Delete(context.TODO(), "labeled-backend", metav1.DeleteOptions{})).Should(Succeed())
		Expect(csB.CoreV1().Services(tenantBNs).Delete(context.TODO(), svcName, metav1.DeleteOptions{})).Should(Succeed())
		Eventually(func() bool {
			_, errClient := csA.CoreV1().Pods(tenantANs).Get(context.TODO(), podName, metav1.GetOptions{})
			_, errBackend := csB.CoreV1().Pods(tenantBNs).Get(context.TODO(), "labeled-backend", metav1.GetOptions{})
			return apierrors.IsNotFound(errors.Join(errClient, errBackend))
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
