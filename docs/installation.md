# Installation

## Requirements

- **Capsule**: Already installed and configured
- **Kubernetes**: Version 1.25 or newer
- **CoreDNS**: As cluster DNS provider

## Quick Install

### 1. Use Pre-built Image

```bash
docker pull ghcr.io/corentinptrl/capsule-coredns:latest
```

### 2. Update CoreDNS Deployment

Update your CoreDNS deployment image in `kube-system` namespace to use the plugin-enabled image.

### 3. Configure Corefile

Add the capsule plugin **before** the kubernetes plugin in your CoreDNS ConfigMap:

```
capsule {
   namespace_labels capsule.io/dns=enabled
   labels capsule.io/expose-dns=true
}
kubernetes cluster.local in-addr.arpa ip6.arpa {
   # ... existing config
}
```

### 4. Restart CoreDNS

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
