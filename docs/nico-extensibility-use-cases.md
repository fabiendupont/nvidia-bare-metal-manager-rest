# NICo Extensible Architecture — Use Cases

**Red Hat NCP Team | April 2026**

---

## Executive Summary

The NICo extensible architecture transforms NICo from a
monolithic infrastructure controller into a platform with
pluggable providers. This document walks through seven use
cases, starting with zero API changes and building toward
full self-service capabilities.

Each use case introduces one new concept from the architecture,
so they can be discussed incrementally with the NICo team.

---

## 1. DPU Cluster Provisioning — Hook Integration

**New concept: Workflow hooks for lifecycle integration**
**API surface change: None — existing site API, new internal hooks**

### The problem

After registering a site in NICo, an operator must manually
create a `DPFHCPProvisioner` custom resource on the management
OpenShift cluster, wait for the Red Hat dpf-hcp-provisioner-
operator to reconcile it through five phases, and verify all
conditions before tenants can use DPU-accelerated networking.

### The solution

A NICo provider registers hooks on the site lifecycle. When a
site reaches `Registered` state, an async hook automatically
starts a Temporal workflow that creates the CR, watches its
progress via the K8s API, and marks the site as DPF-ready.
A sync pre-hook prevents instance creation until the DPU
infrastructure is operational.

No new REST endpoints are needed — the existing site API
triggers everything.

### How it works

```
Existing NICo API            New: Hook + Workflow           External
                                                            
POST /site                   post-create-site               
  name: alpha         ──────► async hook fires              
  status: Registered          starts Temporal workflow       
                                                            
                              DPFHCPProvisioningWorkflow     
                              1. Validate site state         
                              2. Create DPFHCPProvisioner ──► K8s API
                                 CR on mgmt cluster          
                              3. Watch phase via K8s API ◄── operator
                                 Pending → Provisioning       reconciles
                                 → IgnitionGenerating         
                                 → Ready                     
                              4. Update site DPF status      
                                                            
POST /instance                                              
  site: alpha          ──────► pre-create-instance           
                               sync hook checks DPF         
                               status for this site          
                                                            
                               DPF Ready?                    
                               Yes → proceed                 
                               No  → 409 "DPU infra          
                                     not ready"              
```

### Key design choice

NICo creates the CR. The operator does the work. NICo treats
the control plane as a black box — it only watches the phase
field. This separation means NICo doesn't need to understand
HyperShift, ignition generation, or CSR approval.

Compatible with Zero Trust mode: the operator generates
ignition, NICo Core handles network-based BFB flashing
independently.

### What this demonstrates

- **Hooks** enable integration without API changes
- **Async reactions** trigger workflows on lifecycle events
- **Sync pre-hooks** enforce safety gates
- Temporal workflows orchestrate multi-step processes
  with retry and timeout handling

### Reference

NEP-0004: DPF HCP Provisioner Provider

---

## 2. Netris Fabric Sync — Complementary Provider

**New concept: Complementary providers alongside built-in features**
**API surface change: None — hook-driven, no new endpoints**

### The problem

NICo manages tenant-level networking (VPCs, subnets). The
physical switch fabric (NVIDIA Spectrum-X, SONiC, Arista) is
managed separately — by Netris or by Ansible playbooks via
AAP. When NICo creates a VPC, an operator must separately
configure matching VRFs on the switches. This manual
coordination delays tenant onboarding and risks drift.

### The solution

A complementary provider reacts to NICo networking events
via hooks and automatically syncs the physical fabric. It
runs alongside nico-networking, not instead of it.

```
NICo event              Hook                    Fabric action
                                                
POST /vpc         ──► post-create-vpc ─────────► Netris Controller
  name: prod           async reaction            API: Create VRF
                                                 "nico-{vpc-id}"
                                                
POST /subnet      ──► pre-create-subnet ───────► Netris IPAM
  cidr: 10.1.0/24     sync hook (can abort)      Check for IP
                                                 conflict
                                                
POST /subnet      ──► post-create-subnet ──────► Netris Controller
                       async reaction            API: Create VNET
```

### Integration boundary

Each layer has a clear owner. The hook system provides the
glue without tight coupling.

```
Physical switches            Netris (or Ansible/AAP)
DPU hardware + OS            DPF
Tenant networking            NICo
Workload networking          OpenShift OVN-K
```

### Alternative: Ansible-driven fabric

The same hook pattern works with AAP instead of Netris.
The ansible-fabric provider launches AAP job templates
using `nvidia.nvue` (Ethernet) and `nvidia.ufm` (InfiniBand)
collections:

```
POST /vpc         ──► post-create-vpc ─────────► AAP Controller
                       async reaction            Launch job template
                                                 nvidia.nvue.vrf
```

### What this demonstrates

- **Complementary providers** add value alongside built-in features
- Same hook mechanism works for different backends (Netris, AAP)
- **IPAM validation** via sync pre-hooks prevents conflicts
- Partners integrate without modifying NICo core

### Reference

NEP-0005: Netris Fabric Provider
NEP-0003: AAP Provider

---

## 3. Storage Provider Stubs — Extension Points

**New concept: Feature stubs with 501 responses, partner implementation path**
**API surface change: New endpoints, returning 501 until a provider is loaded**

### The problem

NICo doesn't manage storage. A partner like VAST Data or
WEKA wants to add storage management to NICo. Today, they
must build a separate service with its own API, auth, and
database. Tenants manage two systems.

### The solution

NICo declares storage as a known feature. Without a provider,
storage endpoints return 501 Not Implemented with a descriptive
JSON body. When a partner provides an implementation, the same
endpoints return real data.

```
Without storage provider:

GET /capabilities
  "storage": {"status": "not_available"}

POST /storage/volumes
  → 501 {"error": "not_implemented",
         "feature": "storage",
         "message": "Feature 'storage' has no provider configured."}


With VAST storage provider loaded:

GET /capabilities
  "storage": {"status": "available",
              "provider": "vast-storage",
              "version": "1.0.0"}

POST /storage/volumes
  → 201 {"id": "vol-123", "size_gb": 1000, ...}
```

### What the partner ships

A container image with a gRPC server implementing the NICo
provider protocol. The operator adds it to the Helm chart:

```yaml
# Helm values.yaml
nico:
  providers:
    vast-storage:
      enabled: true
      image: registry.vast.io/nico-provider:1.0
      config:
        endpoint: https://vast-mgmt.local
```

```
NICo Pod
+------------------------------------------+
|                                          |
|  +----------------+  +----------------+  |
|  | NICo REST      |  | VAST Storage   |  |
|  | (core +        |  | Provider       |  |
|  |  built-in      |◄►| (partner       |  |
|  |  providers)    |  |  container)    |  |
|  +----------------+  +----------------+  |
|      gRPC over Unix domain socket        |
+------------------------------------------+
```

The partner writes a provider in any gRPC language. NICo
proxies HTTP requests to the provider, applying auth
middleware first. The partner never handles authentication.

### What this demonstrates

- **Feature stubs** document extension points in the live API
- **Capability discovery** tells clients what's available
- **External providers** let partners ship independently
- NICo's auth, tenancy, and rate limiting apply automatically

### Reference

NEP-0001: Extensible Architecture (stubs, capability discovery)
NEP-0008: External Provider Sidecar Protocol

---

## 4. Fault Management — Operational Capabilities

**New concept: Expanding a built-in provider with new features**
**API surface change: New /health/events and /service-events endpoints**

### The problem

GPU faults, NVSwitch failures, power issues, and DPU errors
are detected by different systems (DCGM, NVSentinel, NHC,
powershelf-manager, nvswitch-manager). An operator must
manually correlate alerts, put machines in maintenance,
execute component-specific remediation, validate recovery,
and restore service. On a 720-GPU deployment, this takes
45 minutes per incident and doesn't scale.

### The solution

The health provider expands from basic /healthz endpoints to
a full fault management system with structured events,
automated remediation, and tenant-facing service events.

### Architecture

```
Fault Sources                NICo Health Provider
                             
DCGM / AlertManager ──────►  POST /health/events/ingest
NVSentinel ────────────────►    Create fault_event
NHC / RHWA ────────────────►    (severity, component,
Powershelf sensors ────────►     classification)
NVSwitch state ────────────►     
DPU agent probes ──────────►   If tenant affected:
                                Create service_event
                                (sanitized, no infra details)
                             
                               post-health-event-ingested
                               async hook ──► Remediation
                                              Workflow
```

### Remediation workflow

```
FaultRemediationWorkflow
  1. Classify    — look up classification mapping
                   (gpu-xid-48 → gpu-reset, max 2 retries)
  2. Isolate     — maintenance mode, cordon K8s node
  3. Remediate   — component-specific action
                   GPU: nvidia-smi --gpu-reset
                   NVSwitch: power cycle via gRPC
                   DPU: HBN restart via site agent
  4. Validate    — DCGM diagnostics (level 3)
  5. Restore     — remove maintenance, uncordon, resolve

  On failure → Escalate → AAP creates ITSM ticket
```

### Two views of the same incident

```
Operator view:                    Tenant view:

GET /health/events                GET /service-events

  id: fault-123                     summary: "1 GPU temporarily
  source: nvsentinel                          unavailable"
  severity: critical                impact: "Automated recovery
  component: gpu                             in progress"
  classification: gpu-xid-48       state: resolved
  machine_id: m-456                 started: 14:32
  state: resolved                   resolved: 14:44
  remediation_attempts: 1           downtime_excluded: true
  mttr: 12 min
                                  No machine IDs, XID codes,
  Full infrastructure details     rack locations, or GPU UUIDs
```

### Prometheus metrics

```
nico_fault_events_open{component="gpu", severity="critical"} 0
nico_fault_events_resolved_total{component="gpu"} 47
nico_fault_mttr_seconds{component="gpu"} 720
```

### AAP integration for escalation

When automated remediation fails, the `post-fault-escalated`
hook triggers an AAP job template that creates an ITSM ticket
with fault details, machine location, and recommended action.

### What this demonstrates

- **Expanding built-in providers** with new features
- **Webhook ingestion** from external alert systems
- **Automated remediation** via Temporal workflows
- **Tenant/operator data isolation** (service events vs fault events)
- **Prometheus metrics** for SLA reporting
- **Hook-driven ITSM integration** via AAP

### Reference

NEP-0007: Health Provider — Fault Management and Service Events

---

## 5. Tenant Self-Service — Catalog and Blueprints

**New concept: Service delivery providers, composable blueprints**
**API surface change: New /catalog, /services, /self endpoints**

### The problem

Tenants cannot provision infrastructure without operator
involvement. There is no service catalog, no order tracking,
no usage visibility. Adding a new service tier requires
writing Go code in the fulfillment provider.

### The solution

Three providers deliver a complete self-service experience:

- **Catalog**: Service templates and composable blueprints
- **Fulfillment**: Order management and DAG-based provisioning
- **Showback**: Per-tenant usage tracking and quota visibility

### Service catalog flow

```
NCP Admin                      Tenant
                               
POST /catalog/blueprints       GET /catalog/blueprints
  name: GPU Training Cluster     → list available services
  parameters:                    → permission status per blueprint
    gpu_count: 1..64             
    isolation: [ns, cluster,   POST /catalog/orders
                bare-metal]      template: GPU Training Cluster
  resources:                     gpu_count: 32
    vpc → subnet → instances     
    → ib-partition (if >8)     GET /catalog/orders/:id
                                 status: Provisioning
POST /catalog/blueprints         progress: "Step 3/5"
  /:id/validate                  
  → validates DAG, types,      GET /services
    expressions, cycles          → list active services
                               
                               GET /services/:id/usage
                                 gpu-hours: 120.5
                               
                               GET /self/quotas
                                 gpu-hours: 120.5 / 1000
```

### Blueprint DAG execution

```
Blueprint "GPU Training Cluster" (gpu_count=32):

Layer 1:  [VPC] ──────────────────────────────────► networkingsvc
Layer 2:  [Subnet] ───────────────────────────────► networkingsvc
Layer 3:  [Instance x4] ─────────────────────────► computesvc
          (parallel)
Layer 4:  [IB Partition] ─────────────────────────► networkingsvc
          (conditional: gpu_count > 8)

On failure: rollback in reverse order
```

The fulfillment provider compiles the blueprint into a
Temporal workflow DAG, resolves `{{ expressions }}`, evaluates
conditions at runtime, and executes resources layer by layer
with parallel execution within each layer.

### Showback — metering via hooks

```
post-create-instance hook ──► StartMetering(tenant, resource, "gpu-hours")
post-delete-instance hook ──► StopMetering(resource)

GET /self/usage              ──► Aggregated metrics from store
GET /self/quotas             ──► Usage vs allocation limits
GET /services/:id/usage      ──► Per-service breakdown
```

### What this demonstrates

- **New API surface** for tenant-facing features
- **Composable blueprints** with DAG-based execution
- **Cross-provider orchestration** (networking + compute in one workflow)
- **Hook-driven metering** for usage tracking
- Can be delivered as **built-in or external providers**

### Reference

NEP-0002: Composable Blueprints
NEP-0001: Service delivery providers (catalog, fulfillment, showback)

---

## 6. Deployment Profiles — Runtime Composition

**New concept: Profiles select which providers load**
**API surface change: GET /capabilities reflects active profile**

### The problem

Different NICo deployments need different capabilities. A
standalone NVIDIA management plane doesn't need a service
catalog. A full NCP needs everything including DPF and fabric
automation. Today, all code runs everywhere.

### The solution

Profiles select which providers load at startup. Same binary,
different configuration. The Helm chart or NICo Operator
maps deployment intent to a profile.

### Profile comparison

```
NICO_PROFILE=management         NICO_PROFILE=ncp

 networking          Y           networking          Y
 compute             Y           compute             Y
 health              Y           site                Y
 nvswitch            Y           health + faults     Y
                                 firmware            Y
 site             [501]          nvswitch            Y
 catalog          [501]          catalog             Y
 fulfillment      [501]          fulfillment         Y
 storage          [501]          showback            Y
 dpf-hcp          [501]          dpf-hcp             Y
 fabric           [501]          ansible-fabric      Y
                                 storage          [501]
 4 providers loaded
 7 features return 501           11 providers loaded
                                 1 feature returns 501
```

### Helm-driven composition

```yaml
# NCP operator or Helm values.yaml

nico:
  profile: ncp
  providers:
    # Core (always)
    nico-networking: {}
    nico-compute: {}
    nico-site: {}
    nico-health:
      fault_management: true

    # Service delivery
    nico-catalog: {}
    nico-fulfillment: {}
    nico-showback: {}

    # Infrastructure extensions
    nico-dpfhcp:
      auto_provision: true

    # Partner (optional)
    netris-fabric:
      enabled: false
      url: https://netris.example.com
    vast-storage:
      enabled: false
```

### Capability-aware clients

```
GET /capabilities → client discovers what's available
                  → UI hides tabs for unavailable features
                  → IaC tools skip unsupported resources
                  → No broken endpoints, only 501 stubs
```

### What this demonstrates

- **Same binary** serves different deployment models
- **Profiles** enforce valid provider combinations
  (dependency resolution catches misconfigurations)
- **Helm/Operator** maps deployment intent to configuration
- **Clients adapt** via capability discovery

### Reference

NEP-0001: Profiles, capability discovery

---

## 7. Infrastructure as Code

**New concept: External IaC tools calling NICo's API**
**API surface change: None — consumers of the existing API**

### The problem

NCPs use Terraform/OpenTofu and Ansible for infrastructure
management. NICo requires its own API calls. There is no
declarative, state-tracked interface for GPU infrastructure.

### The solution

An OpenTofu provider and Ansible collection — both generated
from the same OpenAPI spec — let NCPs manage NICo resources
with their existing IaC workflows.

### OpenTofu — declarative, state-tracked (day-1)

```hcl
resource "nico_vpc" "prod" {
  name    = "production"
  site_id = data.nico_site.alpha.id
}

resource "nico_instance" "gpu" {
  count              = 4
  name               = "gpu-worker-${count.index}"
  vpc_id             = nico_vpc.prod.id
  instance_type_id   = data.nico_instance_type.dgx.id
  operating_system_id = data.nico_operating_system.rhel.id
}

# terraform plan  → shows diff against NICo state
# terraform apply → creates via NICo REST API
# terraform destroy → cleans up
```

### Ansible — imperative, task-oriented (day-2)

```yaml
- hosts: localhost
  module_defaults:
    group/nvidia.bare_metal.all:
      api_url: "{{ nico_url }}"
      api_token: "{{ nico_token }}"
      org: "{{ org }}"
  tasks:
    - nvidia.bare_metal.instance_info:
        filters:
          status: Ready
      register: instances

    - nvidia.bare_metal.machine:
        id: "{{ item.machine_id }}"
        labels:
          patched: "2026-04-09"
      loop: "{{ instances.resources }}"
```

### Dynamic inventory

```yaml
# inventory/nico.bmm.yml
plugin: nvidia.bare_metal.bmm
filters:
  status: Ready
group_by_labels: true
```

The `nvidia.bare_metal` collection provides 56 modules
covering every NICo resource type, generated from the
OpenAPI spec. It includes a dynamic inventory plugin that
discovers instances from the NICo API.

### Code generation pipeline

```
openapi/spec.yaml
       |
       +──► OpenTofu provider (Go, Terraform Plugin Framework)
       |
       +──► Ansible collection (Python, auto-generated modules)
       |
       +──► Client SDKs (Go, Python)
```

Same source of truth, multiple output formats.

### What this demonstrates

- **NICo's REST API is the integration surface** — no special
  protocol needed for IaC tools
- **Code generation** from OpenAPI keeps all tools in sync
- **Day-1** (provisioning) and **day-2** (operations) covered
- NCPs use their existing workflows, not NICo-specific tooling

### Reference

NEP-0006: OpenTofu/Terraform Provider

---

## Architecture Summary

```
+------------------------------------------------------------------+
|                         NICo Core                                |
|  Identity | Multi-tenancy | API Framework | Workflows | Storage  |
|  Provider Registry | Hook System | Capability Discovery          |
+------------------------------------------------------------------+
      |              |              |              |
      v              v              v              v
+----------+  +-----------+  +-----------+  +-----------+
| Built-in |  | Service   |  | Infra     |  | Partner   |
| Providers|  | Delivery  |  | Extension |  | Providers |
|          |  |           |  |           |  |           |
| network  |  | catalog   |  | dpf-hcp   |  | netris    |
| compute  |  | fulfill   |  | ansible   |  | vast      |
| site     |  | showback  |  | fabric    |  | weka      |
| health   |  |           |  |           |  | netbox    |
| firmware |  | blueprints|  | fault     |  |           |
| nvswitch |  | DAG exec  |  | mgmt     |  |           |
+----------+  +-----------+  +-----------+  +-----------+
      |              |              |              |
      +--------------+--------------+--------------+
                           |
                     Hook System
              sync (validate, gate)
              async (Temporal signals)
              orchestration (child workflows)
                           |
      +--------------+--------------+--------------+
      |              |              |              |
  REST API    OpenTofu Provider  Ansible      Webhooks
                               Collection
```

---

## Enhancement Proposals

| NEP | Title | Status | Use Case |
|-----|-------|--------|----------|
| 0001 | Extensible Architecture | Implemented | All |
| 0002 | Composable Blueprints | Implemented | 5 |
| 0003 | AAP Provider | Implemented | 2, 4 |
| 0004 | DPF HCP Provider | Implemented | 1 |
| 0005 | Netris Fabric Provider | Implemented | 2 |
| 0006 | OpenTofu Provider | Proposal | 7 |
| 0007 | Fault Management | Implemented | 4 |
| 0008 | External Provider Sidecars | Proposal | 3 |

---

## Impact

| Use Case | Who benefits | Key metric |
|----------|-------------|------------|
| 1. DPU provisioning | Operators | Site bring-up: manual → automated |
| 2. Fabric sync | Operators, Partners | Config drift: eliminated |
| 3. Storage stubs | Partners | Integration: months → days |
| 4. Fault management | SREs, Tenants | MTTR: 45 min → 12 min |
| 5. Self-service | Tenants | Provisioning: hours → minutes |
| 6. Profiles | NCP Operators | Deployment: one-size → composable |
| 7. IaC | Platform Engineers | Tooling: custom → standard |
