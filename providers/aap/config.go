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

package aap

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// HookBinding maps a NICo lifecycle event to an AAP job template.
type HookBinding struct {
	Event         string        `yaml:"event"`
	Feature       string        `yaml:"feature"`
	Type          string        `yaml:"type"` // "sync" or "async"
	TemplateName  string        `yaml:"template"`
	ExtraVarsFrom string        `yaml:"extra_vars_from"`
	Timeout       time.Duration `yaml:"timeout"`
}

// Config holds the AAP provider configuration.
type Config struct {
	ControllerURL string        `yaml:"controller_url"`
	Token         string        `yaml:"token"`
	Organization  string        `yaml:"organization"`
	Bindings      []HookBinding `yaml:"bindings"`
}

// LoadConfigFromEnv reads AAP provider configuration from environment variables.
// AAP_CONTROLLER_URL, AAP_TOKEN, and AAP_ORGANIZATION are read directly.
// AAP_BINDINGS_FILE points to a YAML file containing hook bindings.
// If no bindings file is specified, default bindings are used.
func LoadConfigFromEnv() (*Config, error) {
	cfg := &Config{
		ControllerURL: os.Getenv("AAP_CONTROLLER_URL"),
		Token:         os.Getenv("AAP_TOKEN"),
		Organization:  os.Getenv("AAP_ORGANIZATION"),
	}

	if cfg.ControllerURL == "" {
		return nil, fmt.Errorf("AAP_CONTROLLER_URL is required")
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("AAP_TOKEN is required")
	}
	if cfg.Organization == "" {
		cfg.Organization = "default"
	}

	bindingsFile := os.Getenv("AAP_BINDINGS_FILE")
	if bindingsFile != "" {
		data, err := os.ReadFile(bindingsFile)
		if err != nil {
			return nil, fmt.Errorf("read bindings file %s: %w", bindingsFile, err)
		}
		if err := yaml.Unmarshal(data, &cfg.Bindings); err != nil {
			return nil, fmt.Errorf("parse bindings file %s: %w", bindingsFile, err)
		}
	} else {
		cfg.Bindings = defaultBindings()
	}

	return cfg, nil
}

// defaultBindings returns the default hook bindings when no bindings file is configured.
func defaultBindings() []HookBinding {
	return []HookBinding{
		{
			Event:         "post-create-instance",
			Feature:       "compute",
			Type:          "async",
			TemplateName:  "post-provision-hardening",
			ExtraVarsFrom: "instance",
		},
		{
			Event:         "pre-delete-instance",
			Feature:       "compute",
			Type:          "sync",
			TemplateName:  "tenant-data-cleanup",
			ExtraVarsFrom: "instance",
			Timeout:       30 * time.Minute,
		},
	}
}
