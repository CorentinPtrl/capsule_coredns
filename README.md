# Capsule CoreDNS Plugin

This repository contains a Capsule plugin for CoreDNS, designed for use with [Capsule](https://projectcapsule.dev/). Capsule provides multi-tenancy and policy management for Kubernetes.

## Feature Description
By default, CoreDNS allows DNS resolution of any pod or service from any other pod in the cluster. This plugin introduces tenant-aware DNS resolution, enabling the following:

- **DNS resolution only within a tenant or a single namespace**: Pods can only resolve services and pods within their own tenant or namespace.
- **Namespace whitelisting**: Specific namespaces (e.g., `default` for `kubernetes.default.svc`) can be whitelisted to allow cross-tenant resolution for essential services.
- **Chaining with the Kubernetes plugin**: The plugin is designed to be used in conjunction with the CoreDNS Kubernetes plugin, leveraging request labels and metadata to enforce resolution policies.

## Features
- Tenant-aware DNS resolution for Kubernetes
- Namespace whitelisting for essential services
- Integration with CoreDNS and the Kubernetes plugin
- Logging and error handling for plugin setup

## Requirements
- Go 1.25 or newer
- CoreDNS (compatible version)
- Kubernetes plugin enabled

## Usage
Configure CoreDNS to use the Capsule plugin by adding it to your Corefile. Ensure the Kubernetes plugin is also loaded, as Capsule depends on it.

Example Corefile snippet:
```
capsule {
    namespace_labels capsule.io/dns=enabled
    labels capsule.io/expose-dns=true
}
kubernetes cluster.local in-addr.arpa ip6.arpa {
   pods insecure
   fallthrough in-addr.arpa ip6.arpa
   ttl 30
}
```
