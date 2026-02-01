# Configuration

The Capsule plugin is configured in the CoreDNS Corefile.

## Syntax

```
capsule {
    namespace_labels <label-selector>
    labels <service-label-selector>
}
```

## Options

### `namespace_labels`

Allows specific namespaces to be accessible from all tenants.

**Example**: Allow access to namespaces with label `capsule.io/dns=enabled`

```
namespace_labels capsule.io/dns=enabled
```

**Use for**:
- System namespaces (default, kube-system)
- Shared monitoring/logging
- Platform services

### `labels`

Allows specific services to be accessible from all tenants.

**Example**: Expose services with label `capsule.io/expose-dns=true`

```
labels capsule.io/expose-dns=true
```

**Use for**:
- Authentication services
- Shared databases
- API gateways
- Platform APIs

## Complete Example

```
.:53 {
    errors
    health
    ready
    capsule {
       namespace_labels capsule.io/dns=enabled
       labels capsule.io/expose-dns=true
    }
    kubernetes cluster.local in-addr.arpa ip6.arpa {
       pods insecure
       fallthrough in-addr.arpa ip6.arpa
       ttl 30
    }
    forward . /etc/resolv.conf
    cache 30
}
```

## Label Selector Formats

- Simple: `key=value`
- Multiple: `key1=value1,key2=value2`
- Set-based: `key in (value1, value2)`

See [Kubernetes label selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors) for details.

## Applying Changes

If the `reload` plugin is enabled, changes are applied automatically.

Otherwise, restart CoreDNS:

```bash
kubectl rollout restart deployment/coredns -n kube-system
```
