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

var _ = Describe("DNS resolution isolation between different tenants", Label("dns"), func() {
	var (
		tenantANs = "tenant-a-isolation-ns"
		tenantBNs = "tenant-b-isolation-ns"
		podName   = "dns-test-pod"
		svcName   = "isolated-service"
	)

	tenantA := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-a-isolation",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "owner-a",
							Kind: "User",
						},
					},
				},
			},
		},
	}

	tenantB := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "tenant-b-isolation",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "owner-b",
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

	It("should block a pod in tenant A from resolving a service in tenant B", func() {
		csA := ownerClient(tenantA.Spec.Owners[0].UserSpec)
		csB := ownerClient(tenantB.Spec.Owners[0].UserSpec)

		By("deploying a service with a backing pod in tenant B's namespace")
		backendPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "backend-pod",
				Namespace: tenantBNs,
				Labels:    map[string]string{"app": "isolated-backend"},
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
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "isolated-backend"},
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

		By("executing nslookup for the service in tenant B - should fail or return empty")
		serviceFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, tenantBNs)
		cmd := []string{"nslookup", serviceFQDN}
		stdout, stderr, err := ExecInPod(csA, tenantANs, podName, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		if err == nil {
			Expect(stdout).ToNot(ContainSubstring(fmt.Sprintf("Name:\t%s", serviceFQDN)))
		}

		By("cleaning up")
		Expect(csA.CoreV1().Pods(tenantANs).Delete(context.TODO(), podName, metav1.DeleteOptions{})).Should(Succeed())
		Expect(csB.CoreV1().Pods(tenantBNs).Delete(context.TODO(), backendPod.Name, metav1.DeleteOptions{})).Should(Succeed())
		Expect(csB.CoreV1().Services(tenantBNs).Delete(context.TODO(), svcName, metav1.DeleteOptions{})).Should(Succeed())
		Eventually(func() bool {
			_, errTenant := csA.CoreV1().Pods(tenantANs).Get(context.TODO(), podName, metav1.GetOptions{})
			_, errNonWhitelisted := csB.CoreV1().Pods(backendPod.Namespace).Get(context.TODO(), backendPod.Name, metav1.GetOptions{})
			return apierrors.IsNotFound(errors.Join(errTenant, errNonWhitelisted))
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
