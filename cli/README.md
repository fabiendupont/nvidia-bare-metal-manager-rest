<!--
SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
SPDX-License-Identifier: Apache-2.0
-->

# Carbide CLI

Command-line client for the NVIDIA Bare Metal Manager REST API. Commands are dynamically generated from the embedded OpenAPI spec at startup.

## Build

```bash
make build
```

The binary is produced at `build/binaries/carbide`.

## Configuration

The CLI reads configuration from flags, environment variables, and `~/.carbide.yaml` (in that order of precedence).

| Flag | Env Var | Config Key | Description |
|------|---------|------------|-------------|
| `--base-url` | `CARBIDE_BASE_URL` | `base-url` | API base URL |
| `--org` | `CARBIDE_ORG` | `org` | Organization name (required) |
| `--token` | `CARBIDE_TOKEN` | `token` | Bearer token |
| `--token-command` | | | Shell command that prints a token |
| `--output` | | | Output format: `json` (default), `yaml`, `table` |
| `--token-url` | `CARBIDE_TOKEN_URL` | `token-url` | OIDC token endpoint URL |
| `--keycloak-url` | `CARBIDE_KEYCLOAK_URL` | `keycloak-url` | Keycloak base URL (constructs token-url if not set) |
| `--realm` | `CARBIDE_REALM` | `realm` | Keycloak realm (default: `carbide-dev`, used with --keycloak-url) |
| `--client-id` | `CARBIDE_CLIENT_ID` | `client-id` | OAuth client ID (default: `carbide-api`) |

## Authentication

### Interactive Login (Password Grant)

With a generic OIDC token endpoint:

```bash
carbide --token-url https://auth.example.com/token login --username admin@example.com
# Password is prompted securely
```

With Keycloak (constructs the token URL automatically):

```bash
carbide --keycloak-url http://localhost:8080 login --username admin@example.com
```

### Non-Interactive Login (Client Credentials)

```bash
carbide --token-url https://auth.example.com/token login \
  --client-secret my-secret
```

Both store the token and refresh token in `~/.carbide.yaml`. Subsequent commands auto-refresh expired tokens.

### NGC API Key

```bash
carbide login --api-key nvapi-xxxx
```

Exchanges the NGC API key at `https://authn.nvidia.com/token` (override with `--authn-url` or `CARBIDE_AUTHN_URL`). The resulting bearer token is saved to `~/.carbide.yaml`.

### Token via Environment

```bash
export CARBIDE_TOKEN=$(curl -s -X POST ... | jq -r .access_token)
export CARBIDE_ORG=test-org
carbide site list
```

## Usage Examples

```bash
# List sites
carbide --org test-org site list

# Get a specific site
carbide --org test-org site get <siteId>

# Create a site with per-field flags
carbide --org test-org site create --name "SJC4"

# Create a site with a JSON file
carbide --org test-org site create --data-file site.json

# Create from stdin
cat site.json | carbide --org test-org site create --data-file -

# Create with inline JSON
carbide --org test-org site create --data '{"name":"SJC4","description":"San Jose"}'

# List instances with filters
carbide --org test-org instance list --status provisioned --page-size 20

# Sub-resource operations
carbide --org test-org allocation constraint create <allocationId> \
  --constraint-type SITE --resource-type-id <siteId> --constraint-value 10

# Output as YAML
carbide --org test-org site list --output yaml

# Output as table
carbide --org test-org site list --output table

# Debug mode (shows HTTP requests and responses)
carbide --org test-org --debug site list
```

## Command Structure

Commands follow the pattern `carbide <resource> [sub-resource] <action> [args] [flags]`.

Resources and actions are derived from the OpenAPI spec tags and operation IDs:

| Spec Pattern | CLI Action |
|---|---|
| `get-all-*` | `list` |
| `get-*` | `get` |
| `create-*` | `create` |
| `update-*` | `update` |
| `delete-*` | `delete` |
| `batch-create-*` | `batch-create` |
| `get-*-status-history` | `status-history` |
| `get-*-stats` | `stats` |

Operations on nested API paths (e.g., `/allocation/{id}/constraint`) appear as sub-resource groups:

```
carbide allocation list
carbide allocation constraint list
carbide allocation constraint create <allocationId>
```

Run `carbide --help` to see all available resource commands, or `carbide <resource> --help` for actions and sub-resources.

## Shell Completion

```bash
# Bash (add to ~/.bashrc)
eval "$(carbide completion bash)"

# Zsh (add to ~/.zshrc)
eval "$(carbide completion zsh)"

# Fish
carbide completion fish > ~/.config/fish/completions/carbide.fish
```
