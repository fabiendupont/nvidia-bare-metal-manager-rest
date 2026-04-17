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

package provider

// CollectOpenAPIFragments gathers OpenAPI spec fragments from all registered
// ExternalProvider instances. Returns a slice of YAML byte slices that can
// be merged into the core OpenAPI spec.
func CollectOpenAPIFragments(registry *Registry) [][]byte {
	var fragments [][]byte
	for _, name := range registry.order {
		if ep, ok := registry.providers[name].(*ExternalProvider); ok {
			if frag := ep.OpenAPIFragment(); len(frag) > 0 {
				fragments = append(fragments, frag)
			}
		}
	}
	return fragments
}
