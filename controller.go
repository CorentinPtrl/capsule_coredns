// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package capsule_coredns

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
)

const (
	PodIPIndex         = "podIPs"
	SvcClusterIPIndex  = "clusterIPs"
	NsIndex            = "name"
	CapsuleTenantLabel = "capsule.clastix.io/tenant"
)

type dnsController struct {
	reverseIpInformers []cache.SharedIndexInformer
	nsInformer         cache.SharedIndexInformer
	stopCh             chan struct{}
	hasSynced          bool
}

func newDNSController() (*dnsController, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	reverseIpInformers := []cache.SharedIndexInformer{}
	factory := informers.NewSharedInformerFactory(clientset, 0)
	podInformer := factory.Core().V1().Pods().Informer()

	err = podInformer.AddIndexers(cache.Indexers{
		PodIPIndex: func(obj any) ([]string, error) {
			//nolint:forcetypeassert
			pod := obj.(*v1.Pod)

			var ips []string
			for _, ip := range pod.Status.PodIPs {
				ips = append(ips, ip.IP)
			}

			return ips, nil
		},
	})
	if err != nil {
		return nil, err
	}

	reverseIpInformers = append(reverseIpInformers, podInformer)
	svcInformer := factory.Core().V1().Services().Informer()

	err = svcInformer.AddIndexers(cache.Indexers{
		SvcClusterIPIndex: func(obj any) ([]string, error) {
			//nolint:forcetypeassert
			svc := obj.(*v1.Service)

			var ips []string

			ips = append(ips, svc.Spec.ClusterIPs...)

			return ips, nil
		},
	})
	if err != nil {
		return nil, err
	}

	reverseIpInformers = append(reverseIpInformers, svcInformer)
	nsInformer := factory.Core().V1().Namespaces().Informer()

	err = nsInformer.AddIndexers(cache.Indexers{
		NsIndex: func(obj any) ([]string, error) {
			//nolint:forcetypeassert
			ns := obj.(*v1.Namespace)
			if ns.Name == "" {
				return []string{}, nil
			}

			return []string{ns.Name}, nil
		},
	})
	if err != nil {
		return nil, err
	}

	return &dnsController{
		reverseIpInformers: reverseIpInformers,
		nsInformer:         nsInformer,
		stopCh:             make(chan struct{}),
	}, nil
}

func (d *dnsController) Start() {
	if d.stopCh == nil {
		d.stopCh = make(chan struct{})
	}

	synced := []cache.InformerSynced{}

	log.Infof("Starting capsule controller")

	for _, ctrl := range d.reverseIpInformers {
		go ctrl.Run(d.stopCh)

		synced = append(synced, ctrl.HasSynced)
	}

	log.Infof("Waiting for controllers to sync")

	if !cache.WaitForCacheSync(d.stopCh, synced...) {
		log.Errorf("failed to sync informers")

		d.hasSynced = false

		return
	}

	d.hasSynced = true

	log.Infof("Synced all required resources")

	<-d.stopCh
}

func (c *dnsController) TenantAuthorized(from string, to string, h Capsule) bool {
	nsFrom, _, err := c.getObjectByIP(from)
	if err != nil || nsFrom == nil {
		return true
	}

	var (
		tenantFrom string
		tenantTo   string
		ok         bool
	)

	if tenantFrom, ok = nsFrom.Labels[CapsuleTenantLabel]; !ok {
		return true
	}

	nsTo, obj, err := c.getObjectByIP(to)
	if err != nil || nsTo == nil {
		return true
	}

	svc, isSvc := obj.(*v1.Service)
	if isSvc && h.labelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(h.labelSelector)
		if err == nil && selector.Matches(labels.Set(svc.Labels)) {
			return true
		}
	}

	if h.namespaceLabelSelector != nil {
		selector, err := metav1.LabelSelectorAsSelector(h.namespaceLabelSelector)
		if err == nil && selector.Matches(labels.Set(nsTo.Labels)) {
			return true
		}
	}

	if tenantTo, ok = nsTo.Labels[CapsuleTenantLabel]; !ok {
		return false
	}

	return tenantFrom == tenantTo
}

func (c *dnsController) HasSynced() bool {
	return c.hasSynced
}

func (c *dnsController) getObjectByIP(ip string) (*v1.Namespace, any, error) {
	for _, informer := range c.reverseIpInformers {
		for _, key := range informer.GetIndexer().ListKeys() {
			objs, err := informer.GetIndexer().ByIndex(key, ip)
			if err != nil || len(objs) == 0 {
				continue
			}

			//nolint:forcetypeassert
			meta := objs[0].(*metav1.ObjectMeta)

<<<<<<< HEAD
			return c.getNSByName(meta.Namespace)
=======
			log.Infof("Found object %s in namespace %s for IP %s", meta.GetName(), meta.GetNamespace(), ip)
			ns, err := c.getNSByName(meta.GetNamespace())

			return ns, objs[0], err
>>>>>>> 1d2acb0 (feat(capsule): labels selector)
		}
	}

	return nil, nil, nil
}

func (c *dnsController) getNSByName(name string) (*v1.Namespace, error) {
	objs, err := c.nsInformer.GetIndexer().ByIndex(NsIndex, name)
	if err != nil || len(objs) == 0 {
		return nil, err
	}

	//nolint:forcetypeassert
	return objs[0].(*v1.Namespace), nil
}
