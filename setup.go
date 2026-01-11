// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package capsule_coredns

import (
	"errors"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/kubernetes"
)

const pluginName = "capsule"

func init() { plugin.Register(pluginName, setup) }

func setup(c *caddy.Controller) error {
	log.Info("setup called")
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		handler := &Capsule{Next: next}

		err := handler.Setup()
		if err != nil {
			log.Errorf("setup error: %v", err)

			return nil
		}

		return handler
	})
	//nolint:forcetypeassert
	c.OnStartup(func() error {
		kubernetesHandler := dnsserver.GetConfig(c).Handler("kubernetes")
		if kubernetesHandler == nil {
			return plugin.Error(pluginName, errors.New("kubernetes plugin not loaded"))
		}

		capsuleHandler := dnsserver.GetConfig(c).Handler("capsule")

		m := capsuleHandler.(*Capsule)
		m.kubernetesHandler = kubernetesHandler.(*kubernetes.Kubernetes)

		log.Info("kubernetes handler assigned to capsule plugin")

		go m.dnsController.Start()

		return nil
	})

	return nil
}
