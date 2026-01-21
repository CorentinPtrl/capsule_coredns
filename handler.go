// Copyright 2020-2025 Project Capsule Authors
// SPDX-License-Identifier: Apache-2.0

package capsule_coredns

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/plugin"
	kubedns "github.com/coredns/coredns/plugin/kubernetes"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var log = clog.NewWithPlugin("capsule")

type Capsule struct {
	Next                   plugin.Handler
	kubernetesHandler      *kubedns.Kubernetes
	dnsController          *dnsController
	labelSelector          *meta.LabelSelector
	namespaceLabelSelector *meta.LabelSelector
}

func (h *Capsule) Setup() error {
	var err error

	h.dnsController, err = newDNSController()
	if err != nil {
		log.Errorf("failed to create DNS controller: %v", err)

		return err
	}

	return nil
}

func (h *Capsule) Parse(c *caddy.Controller) error {
	for c.NextBlock() {
		switch c.Val() {
		case "labels":
			args := c.RemainingArgs()
			if len(args) > 0 {
				labelSelectorString := strings.Join(args, " ")
				ls, err := meta.ParseToLabelSelector(labelSelectorString)
				if err != nil {
					return fmt.Errorf("unable to parse label selector value: '%v': %v", labelSelectorString, err)
				}
				h.labelSelector = ls
				continue
			}
			return c.ArgErr()
		case "namespace_labels":
			args := c.RemainingArgs()
			if len(args) > 0 {
				namespaceLabelSelectorString := strings.Join(args, " ")
				nls, err := meta.ParseToLabelSelector(namespaceLabelSelectorString)
				if err != nil {
					return fmt.Errorf("unable to parse namespace_label selector value: '%v': %v", namespaceLabelSelectorString, err)
				}
				h.namespaceLabelSelector = nls
				continue
			}
			return c.ArgErr()
		default:
			return c.Errf("unknown property '%s'", c.Val())
		}
	}
	return nil
}

func (h *Capsule) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	qname := state.QName()

	zone := plugin.Zones(h.kubernetesHandler.Zones).Matches(qname)
	if zone == "" {
		return plugin.NextOrFailure(h.kubernetesHandler.Name(), h.kubernetesHandler.Next, ctx, w, r)
	}

	zone = qname[len(qname)-len(zone):] // maintain case of original query
	state.Zone = zone

	destIp := state.IP()

	if !h.dnsController.HasSynced() {
		return plugin.BackendError(ctx, h.kubernetesHandler, zone, dns.RcodeServerFailure, state, nil, plugin.Options{})
	}

	destIp, err := h.GetDestIp(ctx, state, zone, destIp)
	if err != nil {
		return h.Next.ServeDNS(ctx, w, r)
	}

	log.Infof("query: %s %s from %s DestIP %s", r.Question[0].Name, dns.TypeToString[r.Question[0].Qtype], state.IP(), destIp)

	if !h.dnsController.TenantAuthorized(state.IP(), destIp) {
		log.Info("blocking request due to tenant isolation policy")
		log.Infof("QName: %s", state.QName())

		return plugin.BackendError(ctx, h.kubernetesHandler, zone, dns.RcodeSuccess, state, nil, plugin.Options{})
	}

	return h.Next.ServeDNS(ctx, w, r)
}

func (h *Capsule) GetDestIp(ctx context.Context, state request.Request, zone string, destIp string) (string, error) {
	switch state.QType() {
	case dns.TypeA:
		records, _, err := plugin.A(ctx, h.kubernetesHandler, zone, state, nil, plugin.Options{})
		if err != nil {
			log.Infof("kubernetes.Records error: %v", err)
		}

		if len(records) == 0 {
			return "", errors.New("kubernetes record not found")
		}

		//nolint:forcetypeassert
		destIp = records[0].(*dns.A).A.String()
	case dns.TypeAAAA:
		records, _, err := plugin.AAAA(ctx, h.kubernetesHandler, zone, state, nil, plugin.Options{})
		if err != nil {
			return "", err
		}

		if len(records) == 0 {
			return "", errors.New("kubernetes record not found")
		}

		//nolint:forcetypeassert
		destIp = records[0].(*dns.AAAA).AAAA.String()
	}

	return destIp, nil
}

func (h *Capsule) Name() string { return pluginName }
