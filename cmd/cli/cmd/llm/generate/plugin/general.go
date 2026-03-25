/*
Copyright 2026 The RBG Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package plugin

import (
	"fmt"
	"strconv"

	corev1 "k8s.io/api/core/v1"
)

const (
	// GeneralPluginName is the name of the general generator plugin.
	GeneralPluginName = "general"
)

// GeneralPlugin implements Plugin for arbitrary configuration generator tools.
// It provides a best-effort parameter mapping using common CLI flag conventions.
// For tools with non-standard interfaces, use ExtraArgs in Config to pass
// additional arguments directly.
type GeneralPlugin struct {
	// toolName is the binary name or path to execute inside the container.
	toolName string
}

// NewGeneralPlugin creates a GeneralPlugin that runs the given tool binary.
func NewGeneralPlugin(toolName string) *GeneralPlugin {
	return &GeneralPlugin{toolName: toolName}
}

// Name returns the plugin name.
func (p *GeneralPlugin) Name() string {
	return GeneralPluginName
}

// Container builds the container spec for the general generator Job.
// Parameters are mapped to common CLI flags on a best-effort basis.
// Storage volumes are mounted by the executor via the storage plugin.
func (p *GeneralPlugin) Container(cfg *Config) (corev1.Container, error) {
	if p.toolName == "" {
		return corev1.Container{}, fmt.Errorf("general plugin: toolName is required")
	}
	if cfg.Image == "" {
		return corev1.Container{}, fmt.Errorf("general plugin: Image is required (no default for general generators)")
	}

	args := p.buildArgs(cfg)

	return corev1.Container{
		Name:    "generate",
		Image:   cfg.Image,
		Command: []string{p.toolName},
		Args:    args,
	}, nil
}

// buildArgs constructs a best-effort argument list from Config.
// Only non-zero/non-empty fields are included.
func (p *GeneralPlugin) buildArgs(cfg *Config) []string {
	args := []string{}

	if cfg.ModelName != "" {
		args = append(args, "--model", cfg.ModelName)
	}
	if cfg.SystemName != "" {
		args = append(args, "--system", cfg.SystemName)
	}
	if cfg.TotalGPUs > 0 {
		args = append(args, "--total-gpus", strconv.Itoa(cfg.TotalGPUs))
	}
	if cfg.BackendName != "" {
		args = append(args, "--backend", cfg.BackendName)
	}
	if cfg.ISL > 0 {
		args = append(args, "--isl", strconv.Itoa(cfg.ISL))
	}
	if cfg.OSL > 0 {
		args = append(args, "--osl", strconv.Itoa(cfg.OSL))
	}
	if cfg.ModelPath != "" {
		args = append(args, "--model-path", cfg.ModelPath)
	}
	if cfg.OutputDir != "" {
		args = append(args, "--output-dir", cfg.OutputDir)
	}

	// Extra tool-specific arguments
	for key, value := range cfg.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s", key), value)
	}

	return args
}
