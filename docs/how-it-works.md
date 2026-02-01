# How It Works

## Overview

The plugin enforces DNS-level isolation between Capsule tenants.

## Authorization Rules

The `TenantAuthorized` function evaluates DNS queries using the following logic:

### Allow Conditions

A DNS query is **allowed** if **any** of these conditions are true:

1. **Source namespace not found** - Cannot resolve source IP to a namespace (returns `true` as fail-open)
2. **No source tenant** - Source namespace lacks `capsule.clastix.io/tenant` label (non-tenant workloads can query anything)
3. **Destination namespace not found** - Cannot resolve target IP to a namespace (returns `true` as fail-open)
4. **Whitelisted service** - Target is a Service matching the `labels` selector in plugin config
5. **Whitelisted namespace** - Target namespace matches `namespace_labels` selector in plugin config
6. **Same tenant** - Both namespaces have matching `capsule.clastix.io/tenant` labels

## How DNS Resolution Works

1. Query arrives at CoreDNS
2. Plugin checks if it's for a Kubernetes zone (`cluster.local`)
3. Waits for informer cache sync
4. Resolves target IP via Kubernetes plugin
5. Identifies source pod's tenant (reverse IP lookup)
6. Identifies target service/pod's tenant
7. Applies authorization rules
8. Allows or blocks the query

## Security Notes

- DNS isolation alone doesn't prevent direct IP access
- Combine with NetworkPolicies for complete isolation
- Denied queries return `NOERROR` (no information disclosure)
- Assumes namespace labels are controlled by admins

## Example Scenarios

**✅ Allowed**:
- Pod in `team-a-frontend` → Service in `team-a-backend` (same tenant)
- Pod in `team-a-app` → Service in `monitoring` (whitelisted namespace)
- Pod in `team-b-app` → Service with expose label (whitelisted service)

**❌ Denied**:
- Pod in `team-a-app` → Service in `team-b-app` (different tenants)
- Pod in `team-a-app` → Private service in `team-b-platform` (not exposed)
