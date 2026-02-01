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
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	capsulev1beta2 "github.com/projectcapsule/capsule/api/v1beta2"
	"github.com/projectcapsule/capsule/pkg/api"
)

var _ = Describe("DNS resolution for pods outside of tenants", Label("dns"), func() {
	var (
		nonTenantNs = "non-tenant-ns"
		tenantNs    = "target-tenant-ns"
		podName     = "non-tenant-pod"
		svcName     = "tenant-service"
	)

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "target-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "frank",
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

		By("creating a non-tenant namespace (no capsule tenant label)", func() {
			ns := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nonTenantNs,
					Labels: map[string]string{
						"env": "e2e",
					},
				},
			}
			Expect(k8sClient.Create(context.TODO(), ns)).Should(Succeed())
		})

		By("creating the tenant namespace", func() {
			ns := NewNamespace(tenantNs)
			NamespaceCreation(ns, tnt.Spec.Owners[0].UserSpec, defaultTimeoutInterval).Should(Succeed())
			TenantNamespaceList(tnt, defaultTimeoutInterval).Should(ContainElement(ns.GetName()))
		})
	})

	JustAfterEach(func() {
		Expect(k8sClient.Delete(context.TODO(), tnt)).Should(Succeed())
		By("deleting namespaces", func() {
			for _, nsName := range []string{nonTenantNs, tenantNs} {
				ns := NewNamespace(nsName)
				err := k8sClient.Delete(context.TODO(), ns)
				if err != nil && !apierrors.IsNotFound(err) {
					Expect(err).ToNot(HaveOccurred())
				}
			}
		})
	})

	It("should allow a pod outside any tenant to resolve services in any namespace", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		By("deploying a service in the tenant namespace")
		backendPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tenant-backend",
				Namespace: tenantNs,
				Labels:    map[string]string{"app": "tenant-backend"},
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
		_, err := cs.CoreV1().Pods(tenantNs).Create(context.TODO(), backendPod, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: tenantNs,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "tenant-backend"},
				Ports: []corev1.ServicePort{{
					Port:       80,
					TargetPort: intstr.FromInt32(80),
				}},
			},
		}
		_, err = cs.CoreV1().Services(tenantNs).Create(context.TODO(), svc, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("deploying a client pod in the non-tenant namespace")
		clientPod := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: nonTenantNs,
				Labels:    map[string]string{"app": "non-tenant-client"},
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
		Expect(k8sClient.Create(context.TODO(), clientPod)).Should(Succeed())

		By("waiting for the client pod to be running")
		Eventually(func() corev1.PodPhase {
			p := &corev1.Pod{}
			_ = k8sClient.Get(context.TODO(), client.ObjectKey{Namespace: nonTenantNs, Name: podName}, p)
			return p.Status.Phase
		}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

		By("executing nslookup for the service in the tenant namespace from a non-tenant pod")
		serviceFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, tenantNs)
		adminCs, err := kubernetes.NewForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		cmd := []string{"nslookup", serviceFQDN}
		stdout, stderr, err := ExecInPod(adminCs, nonTenantNs, podName, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdout).To(ContainSubstring(fmt.Sprintf("Name:\t%s", serviceFQDN)))
		Expect(stdout).To(MatchRegexp(`Address: [0-9.]+`))

		By("cleaning up")
		Expect(k8sClient.Delete(context.TODO(), clientPod)).Should(Succeed())
		Expect(cs.CoreV1().Pods(tenantNs).Delete(context.TODO(), "tenant-backend", metav1.DeleteOptions{})).Should(Succeed())
		Expect(cs.CoreV1().Services(tenantNs).Delete(context.TODO(), svcName, metav1.DeleteOptions{})).Should(Succeed())
		Eventually(func() bool {
			_, errTenant := cs.CoreV1().Pods(backendPod.Namespace).Get(context.TODO(), backendPod.Name, metav1.GetOptions{})
			errNonWhitelisted := k8sClient.Get(context.TODO(), types.NamespacedName{Namespace: clientPod.Namespace, Name: clientPod.Name}, &corev1.Pod{})
			return apierrors.IsNotFound(errors.Join(errTenant, errNonWhitelisted))
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
