# Installation

## Requirements

- **Capsule**: Already installed and configured
- **Kubernetes**: Version 1.25 or newer
- **CoreDNS**: As cluster DNS provider

## Quick Install

### 1. Use Pre-built Image

Replace the CoreDNS image in the `deployment/coredns` with the Capsule-enabled version:
   ```diff
   - image: registry.k8s.io/coredns/coredns:v1.13.2
   + image: ghcr.io/corentinptrl/capsule_coredns:latest
   ```

### 2. Configure Corefile

Add the capsule plugin in your CoreDNS ConfigMap:

```
capsule {
   namespace_labels capsule.io/dns=enabled
   labels capsule.io/expose-dns=true
}
kubernetes cluster.local in-addr.arpa ip6.arpa {
   # ... existing config
}
```

### 3. Restart CoreDNS

```bash
kubectl rollout restart deployment/coredns -n kube-system
```

## Verification

Check logs for successful startup:

```bash
kubectl logs -n kube-system deployment/coredns | grep capsule
```

## Build From Source

To build your own CoreDNS image with the plugin, add to `plugin.cfg` before `kubernetes`:

```
capsule:github.com/projectcapsule/capsule-coredns
```

Then build CoreDNS normally.
