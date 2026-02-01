# Capsule CoreDNS Plugin

DNS-level isolation for multi-tenant Kubernetes clusters using [Capsule](https://projectcapsule.dev/).

## What It Does

Prevents pods in different tenants from discovering each other via DNS while allowing controlled access to shared services.

## Quick Links

-  [Installation](docs/installation.md)
- ️ [Configuration](docs/config.md)
-  [How It Works](docs/how-it-works.md)

## Key Features

- **Tenant Isolation**: DNS queries blocked between different tenants
- **Namespace Whitelisting**: Allow access to shared namespaces (monitoring, logging)
- **Service Whitelisting**: Expose specific services across tenants
- **Zero Configuration for Non-Tenants**: Gradual adoption supported

## Quick Start

1. Replace the CoreDNS image in the `deployment/coredns` with the Capsule-enabled version:
   ```diff
   - image: registry.k8s.io/coredns/coredns:v1.13.2
   + image: ghcr.io/corentinptrl/capsule_coredns:latest
   ```

2. Configure in Corefile (before kubernetes plugin):
   ```
   capsule {
      namespace_labels capsule.io/dns=enabled
      labels capsule.io/expose-dns=true
   }
   ```

## Requirements

- Kubernetes 1.25+
- Capsule installed
- CoreDNS as cluster DNS

## License

Apache 2.0 - See [LICENSE](LICENSE)
