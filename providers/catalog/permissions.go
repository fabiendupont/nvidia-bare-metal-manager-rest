/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package catalog

import (
	"sort"
	"strings"
)

// Permission represents a single resource-level permission.
type Permission struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
}

// RoleSpec describes the minimum role needed to order a blueprint.
type RoleSpec struct {
	BlueprintID   string       `json:"blueprint_id"`
	BlueprintName string       `json:"blueprint_name"`
	Permissions   []Permission `json:"permissions"`
}

// resourceTypePermissions maps NICo resource types to the permissions
// required to create them.
var resourceTypePermissions = map[string][]Permission{
	"nico/vpc": {
		{Resource: "vpc", Action: "create"},
		{Resource: "vpc", Action: "read"},
	},
	"nico/subnet": {
		{Resource: "subnet", Action: "create"},
		{Resource: "subnet", Action: "read"},
	},
	"nico/instance": {
		{Resource: "instance", Action: "create"},
		{Resource: "instance", Action: "read"},
	},
	"nico/allocation": {
		{Resource: "allocation", Action: "create"},
		{Resource: "allocation", Action: "read"},
	},
	"nico/network-security-group": {
		{Resource: "network-security-group", Action: "create"},
		{Resource: "network-security-group", Action: "read"},
	},
	"nico/vpc-peering": {
		{Resource: "vpc-peering", Action: "create"},
		{Resource: "vpc-peering", Action: "read"},
	},
	"nico/infiniband-partition": {
		{Resource: "infiniband-partition", Action: "create"},
		{Resource: "infiniband-partition", Action: "read"},
	},
	"nico/nvlink-partition": {
		{Resource: "nvlink-partition", Action: "create"},
		{Resource: "nvlink-partition", Action: "read"},
	},
	"nico/ip-block": {
		{Resource: "ip-block", Action: "create"},
		{Resource: "ip-block", Action: "read"},
	},
	"nico/ssh-key-group": {
		{Resource: "ssh-key-group", Action: "create"},
		{Resource: "ssh-key-group", Action: "read"},
	},
	"nico/operating-system": {
		{Resource: "operating-system", Action: "read"},
	},
	"nico/site": {
		{Resource: "site", Action: "read"},
	},
}

// ExtractPermissions walks a blueprint's resources and returns the
// deduplicated set of permissions required to provision it.
func ExtractPermissions(b *Blueprint, store BlueprintStoreInterface) []Permission {
	seen := make(map[string]bool)
	var perms []Permission

	var walk func(resources map[string]BlueprintResource, depth int)
	walk = func(resources map[string]BlueprintResource, depth int) {
		if depth > MaxNestingDepth {
			return
		}
		for _, res := range resources {
			if strings.HasPrefix(res.Type, "blueprint/") {
				ref := res.Type[10:]
				child, err := lookupBlueprint(ref, store)
				if err != nil {
					continue
				}
				walk(child.Resources, depth+1)
				continue
			}
			if rp, ok := resourceTypePermissions[res.Type]; ok {
				for _, p := range rp {
					key := p.Resource + ":" + p.Action
					if !seen[key] {
						seen[key] = true
						perms = append(perms, p)
					}
				}
			}
		}
	}

	walk(b.Resources, 0)

	sort.Slice(perms, func(i, j int) bool {
		if perms[i].Resource != perms[j].Resource {
			return perms[i].Resource < perms[j].Resource
		}
		return perms[i].Action < perms[j].Action
	})

	return perms
}

// GenerateRoleSpec produces a RoleSpec describing the minimum permissions
// needed to order a given blueprint.
func GenerateRoleSpec(b *Blueprint, store BlueprintStoreInterface) RoleSpec {
	return RoleSpec{
		BlueprintID:   b.ID,
		BlueprintName: b.Name,
		Permissions:   ExtractPermissions(b, store),
	}
}
