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

package fulfillment

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var exprPattern = regexp.MustCompile(`\{\{\s*([^}]+)\s*\}\}`)

// BlueprintResource mirrors catalog.BlueprintResource for DAG compilation.
type BlueprintResource struct {
	Type       string
	DependsOn  []string
	Condition  string
	Count      string
	Properties map[string]interface{}
}

// BlueprintParameter mirrors catalog.BlueprintParameter for sub-blueprint
// expansion without importing the catalog package (avoiding circular imports).
type BlueprintParameter struct {
	Name    string
	Type    string
	Default interface{}
}

// BlueprintLookupFunc resolves a sub-blueprint reference by name and returns
// its resources and parameters. Used during DAG compilation to expand
// "blueprint/..." resource types into inline resources.
type BlueprintLookupFunc func(name string) ([]BlueprintResource, []BlueprintParameter, error)

// maxExpansionDepth limits nested blueprint expansion to prevent infinite recursion.
const maxExpansionDepth = 5

// DAGNode represents a resource in the execution graph.
type DAGNode struct {
	Name       string
	Type       string
	DependsOn  []string
	Condition  string
	Count      int
	Properties map[string]interface{}
}

// DAG represents the compiled execution graph.
type DAG struct {
	Nodes map[string]*DAGNode
	Order [][]string // topologically sorted, grouped for parallel execution
}

// CompileDAG takes a blueprint's resources and parameters, resolves
// expressions, evaluates counts, and returns a DAG ready for execution.
// If lookupFn is non-nil, "blueprint/..." resource types are expanded
// inline by recursively resolving sub-blueprints up to maxExpansionDepth.
func CompileDAG(resources map[string]BlueprintResource, params map[string]interface{}) (*DAG, error) {
	return CompileDAGWithLookup(resources, params, nil)
}

// CompileDAGWithLookup compiles a DAG with optional sub-blueprint expansion.
func CompileDAGWithLookup(resources map[string]BlueprintResource, params map[string]interface{}, lookupFn BlueprintLookupFunc) (*DAG, error) {
	// Expand sub-blueprint references before building nodes
	expanded, err := expandBlueprints(resources, params, lookupFn, 0)
	if err != nil {
		return nil, err
	}

	nodes := make(map[string]*DAGNode, len(expanded))

	for name, res := range expanded {
		count := 1
		if res.Count != "" {
			resolved, err := resolveExprInt(res.Count, params)
			if err != nil {
				return nil, fmt.Errorf("resource %q: invalid count expression: %w", name, err)
			}
			count = resolved
		}

		props := make(map[string]interface{}, len(res.Properties))
		for k, v := range res.Properties {
			props[k] = resolveExprValue(v, params)
		}

		nodes[name] = &DAGNode{
			Name:       name,
			Type:       res.Type,
			DependsOn:  res.DependsOn,
			Condition:  res.Condition,
			Count:      count,
			Properties: props,
		}
	}

	// Topological sort into parallel layers
	order, err := topoSort(nodes)
	if err != nil {
		return nil, err
	}

	return &DAG{Nodes: nodes, Order: order}, nil
}

// expandBlueprints recursively replaces "blueprint/..." resources with the
// child blueprint's resources, prefixing names with the parent resource name.
// Properties from the parent resource are passed as parameter values to the
// child blueprint's parameter defaults.
func expandBlueprints(resources map[string]BlueprintResource, params map[string]interface{}, lookupFn BlueprintLookupFunc, depth int) (map[string]BlueprintResource, error) {
	if depth > maxExpansionDepth {
		return nil, fmt.Errorf("sub-blueprint nesting exceeds maximum depth of %d", maxExpansionDepth)
	}
	if lookupFn == nil {
		return resources, nil
	}

	result := make(map[string]BlueprintResource, len(resources))

	for name, res := range resources {
		if !strings.HasPrefix(res.Type, "blueprint/") {
			result[name] = res
			continue
		}

		ref := res.Type[10:] // strip "blueprint/" prefix
		childResources, childParams, err := lookupFn(ref)
		if err != nil {
			return nil, fmt.Errorf("resource %q: failed to resolve sub-blueprint %q: %w", name, ref, err)
		}

		// Build child parameter values: defaults overridden by parent properties
		childParamValues := make(map[string]interface{})
		for _, cp := range childParams {
			if cp.Default != nil {
				childParamValues[cp.Name] = cp.Default
			}
		}
		for k, v := range res.Properties {
			childParamValues[k] = v
		}

		// Merge child param values into the main params for expression resolution
		mergedParams := make(map[string]interface{}, len(params)+len(childParamValues))
		for k, v := range params {
			mergedParams[k] = v
		}
		for k, v := range childParamValues {
			mergedParams[k] = v
		}

		// Convert child resources slice to a map keyed by prefixed name
		childMap := make(map[string]BlueprintResource, len(childResources))
		for i, cr := range childResources {
			childName := fmt.Sprintf("%s/%s", name, cr.Type)
			if i < len(childResources) {
				// Use a stable name: parent/childIndex if no better name
				childName = fmt.Sprintf("%s/res-%d", name, i)
			}
			// Rewrite depends_on to use prefixed names
			var prefixedDeps []string
			for _, dep := range cr.DependsOn {
				prefixedDeps = append(prefixedDeps, name+"/"+dep)
			}
			// Also inherit parent's depends_on for the first layer (no deps)
			if len(cr.DependsOn) == 0 {
				prefixedDeps = append(prefixedDeps, res.DependsOn...)
			}

			childMap[childName] = BlueprintResource{
				Type:       cr.Type,
				DependsOn:  prefixedDeps,
				Condition:  cr.Condition,
				Count:      cr.Count,
				Properties: cr.Properties,
			}
		}

		// Recursively expand any nested blueprint references
		expandedChildren, err := expandBlueprints(childMap, mergedParams, lookupFn, depth+1)
		if err != nil {
			return nil, err
		}

		for k, v := range expandedChildren {
			result[k] = v
		}
	}

	return result, nil
}

func topoSort(nodes map[string]*DAGNode) ([][]string, error) {
	remaining := make(map[string]*DAGNode, len(nodes))
	for k, v := range nodes {
		remaining[k] = v
	}

	resolved := make(map[string]bool)
	var layers [][]string

	for len(remaining) > 0 {
		var layer []string
		for name, node := range remaining {
			ready := true
			for _, dep := range node.DependsOn {
				if !resolved[dep] {
					ready = false
					break
				}
			}
			if ready {
				layer = append(layer, name)
			}
		}
		if len(layer) == 0 {
			return nil, fmt.Errorf("circular dependency detected in DAG")
		}
		for _, name := range layer {
			resolved[name] = true
			delete(remaining, name)
		}
		layers = append(layers, layer)
	}
	return layers, nil
}

func resolveExprValue(v interface{}, params map[string]interface{}) interface{} {
	s, ok := v.(string)
	if !ok {
		return v
	}
	return exprPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])
		if val, ok := params[inner]; ok {
			return fmt.Sprintf("%v", val)
		}
		// Resource references (e.g., {{ vpc.id }}) are resolved at execution
		// time when the referenced resource's outputs are available
		return match
	})
}

func resolveExprInt(expr string, params map[string]interface{}) (int, error) {
	expr = strings.TrimSpace(expr)
	inner := exprPattern.FindStringSubmatch(expr)
	if inner == nil {
		return strconv.Atoi(expr)
	}

	parts := strings.Fields(inner[1])
	if len(parts) == 1 {
		if val, ok := params[parts[0]]; ok {
			switch v := val.(type) {
			case int:
				return v, nil
			case float64:
				return int(v), nil
			}
		}
		return strconv.Atoi(parts[0])
	}

	// Simple binary expression: "param / value" or "param * value"
	if len(parts) == 3 {
		left, err := resolveOperand(parts[0], params)
		if err != nil {
			return 0, err
		}
		right, err := resolveOperand(parts[2], params)
		if err != nil {
			return 0, err
		}
		switch parts[1] {
		case "/":
			if right == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return left / right, nil
		case "*":
			return left * right, nil
		case "+":
			return left + right, nil
		case "-":
			return left - right, nil
		}
	}

	return 0, fmt.Errorf("cannot resolve expression %q", expr)
}

func resolveOperand(s string, params map[string]interface{}) (int, error) {
	if val, ok := params[s]; ok {
		switch v := val.(type) {
		case int:
			return v, nil
		case float64:
			return int(v), nil
		}
	}
	return strconv.Atoi(s)
}

// EvaluateCondition evaluates a simple boolean condition expression.
func EvaluateCondition(condition string, params map[string]interface{}) bool {
	if condition == "" {
		return true
	}

	inner := exprPattern.FindStringSubmatch(condition)
	if inner == nil {
		return condition == "true"
	}

	parts := strings.Fields(inner[1])
	if len(parts) == 3 {
		left, err := resolveOperand(parts[0], params)
		if err != nil {
			return false // default to false if we can't evaluate — don't create resources on error
		}
		right, err := resolveOperand(parts[2], params)
		if err != nil {
			return true
		}
		switch parts[1] {
		case ">":
			return left > right
		case "<":
			return left < right
		case ">=":
			return left >= right
		case "<=":
			return left <= right
		case "==":
			return left == right
		case "!=":
			return left != right
		}
	}

	return true
}
