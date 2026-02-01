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

var _ = Describe("DNS resolution within the same namespace", Label("dns"), func() {
	var (
		nsName     = "same-ns-dns-test"
		clientPod  = "client-pod"
		backendPod = "backend-pod"
		svcName    = "internal-service"
	)

	tnt := &capsulev1beta2.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name: "same-ns-tenant",
		},
		Spec: capsulev1beta2.TenantSpec{
			Owners: api.OwnerListSpec{
				{
					CoreOwnerSpec: api.CoreOwnerSpec{
						UserSpec: api.UserSpec{
							Name: "bob",
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

	It("should allow a pod to resolve a service within the same namespace", func() {
		cs := ownerClient(tnt.Spec.Owners[0].UserSpec)

		By("deploying a backend pod and service in the namespace")
		backend := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      backendPod,
				Namespace: nsName,
				Labels:    map[string]string{"app": "internal-backend"},
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
		_, err := cs.CoreV1().Pods(nsName).Create(context.TODO(), backend, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		svc := &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      svcName,
				Namespace: nsName,
			},
			Spec: corev1.ServiceSpec{
				Selector: map[string]string{"app": "internal-backend"},
				Ports: []corev1.ServicePort{{
					Port:       80,
					TargetPort: intstr.FromInt32(80),
				}},
			},
		}
		_, err = cs.CoreV1().Services(nsName).Create(context.TODO(), svc, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("deploying a client pod in the same namespace")
		client := &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clientPod,
				Namespace: nsName,
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
		_, err = cs.CoreV1().Pods(nsName).Create(context.TODO(), client, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("waiting for the client pod to be running")
		Eventually(func() corev1.PodPhase {
			p, _ := cs.CoreV1().Pods(nsName).Get(context.TODO(), clientPod, metav1.GetOptions{})
			return p.Status.Phase
		}, 60*time.Second, 2*time.Second).Should(Equal(corev1.PodRunning))

		By("executing nslookup for the service using FQDN")
		serviceFQDN := fmt.Sprintf("%s.%s.svc.cluster.local", svcName, nsName)
		cmd := []string{"nslookup", serviceFQDN}
		stdout, stderr, err := ExecInPod(cs, nsName, clientPod, "busybox", cmd)
		_, _ = fmt.Fprintf(GinkgoWriter, "\nnslookup stdout: %s\nnslookup stderr: %s\n", stdout, stderr)
		Expect(err).ToNot(HaveOccurred())
		Expect(stdout).To(ContainSubstring(fmt.Sprintf("Name:\t%s", serviceFQDN)))
		Expect(stdout).To(MatchRegexp(`Address: [0-9.]+`))

		By("cleaning up")
		Expect(cs.CoreV1().Pods(nsName).Delete(context.TODO(), clientPod, metav1.DeleteOptions{})).Should(Succeed())
		Expect(cs.CoreV1().Pods(nsName).Delete(context.TODO(), backendPod, metav1.DeleteOptions{})).Should(Succeed())
		Expect(cs.CoreV1().Services(nsName).Delete(context.TODO(), svcName, metav1.DeleteOptions{})).Should(Succeed())
		Eventually(func() bool {
			_, errClient := cs.CoreV1().Pods(nsName).Get(context.TODO(), clientPod, metav1.GetOptions{})
			_, errBackend := cs.CoreV1().Pods(nsName).Get(context.TODO(), backendPod, metav1.GetOptions{})
			return apierrors.IsNotFound(errors.Join(errClient, errBackend))
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
	})
})
