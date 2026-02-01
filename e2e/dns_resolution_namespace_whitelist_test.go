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

var _ = Describe("DNS resolution with namespace label whitelisting", Label("dns"), func() {
	var (
		tenantNs      = "tenant-whitelist-ns"
		whitelistedNs = "whitelisted-shared-ns"
		podName       = "dns-test-pod"
		svcName       = "shared-service"
	)

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "whitelist-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "dave",
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

		By("creating the tenant namespace", func() {
			ns := NewNamespace(tenantNs)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		})

		By("creating the whitelisted namespace with the appropriate label", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: whitelistedNs,
					Labels: map[string]string{
						"env":            "e2e",
						"capsule.io/dns": "enabled",
					},
				},
			}
			Expect(k8sClient.Create(context.TODO(), ns)).Should(Succeed())
		})
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		By("deleting namespaces", func() {
			for _, nsName := range []string{tenantNs, whitelistedNs} {
				ns := NewNamespace(nsName)
				err := k8sClient.Delete(context.TODO(), ns)
				if err != nil && !apierrors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})
	})

	It("should allow a tenant pod to resolve services in a whitelisted namespace", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		By("deploying a service in the whitelisted namespace")
		backendPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "shared-backend",
				Namespace: whitelistedNs,
				Labels:    map[string]string{"app": "shared-backend"},
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
				Namespace: whitelistedNs,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "shared-backend"},
				Ports: []corev1.ServicePort{{
					Port:       80,
					TargetPort: intstr.FromInt32(80),
				}},
			},
		}
		Expect(k8sClient.Create(context.TODO(), svc)).Should(Succeed())

		By("deploying a client pod in the tenant namespace")
		clientPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: tenantNs,
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
		_, err := cs.CoreV1().Pods(tenantNs).Create(context.TODO(), clientPod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the client pod to be running")
		Eventually(func() corev1.PodPhase {
			p, _ := cs.CoreV1().Pods(tenantNs).Get(context.TODO(), podName, metav1.GetOptions{})
			return p.Status.Phase
		}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

		By("executing nslookup for the service in the whitelisted namespace")
		serviceFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, whitelistedNs)
		cmd := []string{"nslookup", serviceFQDN}
		stdout, stderr, err := ExecInPod(cs, tenantNs, podName, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdout).To(ContainSubstring(fmt.Sprintf("Name:\t%s", serviceFQDN)))
		Expect(stdout).To(MatchRegexp(`Address: [0-9.]+`))

		By("cleaning up")
		Expect(cs.CoreV1().Pods(tenantNs).Delete(context.TODO(), podName, metav1.DeleteOptions{})).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), backendPod)).Should(Succeed())
		Expect(k8sClient.Delete(context.TODO(), svc)).Should(Succeed())
		Eventually(func() bool {
			_, errTenant := cs.CoreV1().Pods(tenantNs).Get(context.TODO(), podName, metav1.GetOptions{})
			errNonWhitelisted := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: backendPod.Namespace, Name: backendPod.Name}, &corev1.Pod{})
			return apierrors.IsNotFound(errors.Join(errTenant, errNonWhitelisted))
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
