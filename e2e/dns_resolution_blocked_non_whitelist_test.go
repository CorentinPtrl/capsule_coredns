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
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("DNS resolution blocked for non-whitelisted namespace from different tenant", Label("dns"), func() {
	var (
		tenantANs      = "tenant-nowhitelist-a-ns"
		nonWhitelistNs = "non-whitelisted-ns"
		podName        = "dns-test-pod"
		svcName        = "private-service"
	)

	tenantA := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "nowhitelist-tenant-a",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "eve",
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

		By("creating the tenant A namespace", func() {
			ns := NewNamespace(tenantANs)
			NamespaceCreation(ns, tenantA.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tenantA, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		})

		By("creating the non-whitelisted namespace (no capsule.io/dns label)", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nonWhitelistNs,
					Labels: map[string]string{
						"env": "e2e",
					},
				},
			}
			Expect(k8sClient.Create(context.TODO(), ns)).Should(Succeed())
		})
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tenantA)).Should(Succeed())
		By("deleting namespaces", func() {
			for _, nsName := range []string{tenantANs, nonWhitelistNs} {
				ns := NewNamespace(nsName)
				err := k8sClient.Delete(context.TODO(), ns)
				if err != nil && !apierrors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})
	})

	It("should block a tenant pod from resolving services in a non-whitelisted, non-tenant namespace", func() {
		cs := ownerClient(tenantA.Spec.Owners[0].UserSpec)

		By("deploying a service in the non-whitelisted namespace")
		backendPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "private-backend",
				Namespace: nonWhitelistNs,
				Labels:    map[string]string{"app": "private-backend"},
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
		Expect(k8sClient.Create(context.TODO(), backendPod)).Should(Succeed())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: nonWhitelistNs,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "private-backend"},
				Ports: []corev1.ServicePort{{
					Port:       80,
					TargetPort: intstr.FromInt32(80),
				}},
			},
		}
		Expect(k8sClient.Create(context.TODO(), svc)).Should(Succeed())

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
		_, err := cs.CoreV1().Pods(tenantANs).Create(context.TODO(), clientPod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the client pod to be running")
		Eventually(func() corev1.PodPhase {
			p, _ := cs.CoreV1().Pods(tenantANs).Get(context.TODO(), podName, metav1.GetOptions{})
			return p.Status.Phase
		}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

		By("executing nslookup for the service in the non-whitelisted namespace - should fail")
		serviceFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, nonWhitelistNs)
		cmd := []string{"nslookup", serviceFQDN}
		stdout, stderr, err := ExecInPod(cs, tenantANs, podName, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		if err == nil {
			Expect(stdout).ToNot(ContainSubstring(fmt.Sprintf("Name:\t%s", serviceFQDN)))
		}

		By("cleaning up")
		Expect(cs.CoreV1().Pods(tenantANs).Delete(context.TODO(), podName, metav1.DeleteOptions{})).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), backendPod)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), svc)).Should(Succeed())
		Eventually(func() bool {
			_, errTenant := cs.CoreV1().Pods(tenantANs).Get(context.TODO(), podName, metav1.GetOptions{})
			errNonWhitelisted := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backendPod.Namespace, Name: backendPod.Name}, &corev1.Pod{})
			return apierrors.IsNotFound(errors.Join(errTenant, errNonWhitelisted))
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
