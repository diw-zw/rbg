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
	// AIConfiguratorPluginName is the name of the aiconfigurator plugin.
	AIConfiguratorPluginName = "aiconfigurator"

	// DefaultAIConfiguratorImage is the placeholder image for the aiconfigurator container.
	// Replace with the actual image path before deployment.
	// TODO: replace with actual image
	DefaultAIConfiguratorImage = "<todo-replace-with-actual-image>"

	// generateRBGYAMLBin is the entrypoint script inside the aiconfigurator image.
	generateRBGYAMLBin = "generate-rbg-yaml.py"
)

// AIConfiguratorPlugin implements Plugin using NVIDIA's aiconfigurator tool.
// It runs generate-rbg-yaml.py inside the container, which orchestrates:
//  1. Running aiconfigurator to generate optimal serving configurations
//  2. Locating and parsing the output directory
//  3. Rendering RBG deployment YAML files to --output-dir
type AIConfiguratorPlugin struct{}

// Name returns the plugin name.
func (p *AIConfiguratorPlugin) Name() string {
	return AIConfiguratorPluginName
}

// Container builds the container spec for the aiconfigurator Job.
// The container runs generate-rbg-yaml.py with parameters derived from cfg.
// Storage volumes are mounted by the executor via the storage plugin.
func (p *AIConfiguratorPlugin) Container(cfg *Config) (corev1.Container, error) {
	if cfg.ModelPath == "" {
		return corev1.Container{}, fmt.Errorf("aiconfigurator plugin: ModelPath is required")
	}
	if cfg.OutputDir == "" {
		return corev1.Container{}, fmt.Errorf("aiconfigurator plugin: OutputDir is required")
	}

	image := cfg.Image
	if image == "" {
		image = DefaultAIConfiguratorImage
	}

	args := p.buildArgs(cfg)

	return corev1.Container{
		Name:    "generate",
		Image:   image,
		Command: []string{generateRBGYAMLBin},
		Args:    args,
	}, nil
}

// buildArgs constructs the argument list for generate-rbg-yaml.py.
// Parameter mapping follows the aiconfigurator CLI conventions used in executor.go.
func (p *AIConfiguratorPlugin) buildArgs(cfg *Config) []string {
	args := []string{
		"--model", cfg.ModelName,
		"--system", cfg.SystemName,
		"--total-gpus", strconv.Itoa(cfg.TotalGPUs),
		"--backend", cfg.BackendName,
		"--isl", strconv.Itoa(cfg.ISL),
		"--osl", strconv.Itoa(cfg.OSL),
		"--ttft", strconv.FormatFloat(cfg.TTFT, 'f', -1, 64),
		"--tpot", strconv.FormatFloat(cfg.TPOT, 'f', -1, 64),
		"--model-path", cfg.ModelPath,
		"--output-dir", cfg.OutputDir,
		"--namespace", cfg.Namespace,
		"--database-mode", cfg.DatabaseMode,
	}

	// Optional parameters
	if cfg.DecodeSystemName != "" && cfg.DecodeSystemName != cfg.SystemName {
		args = append(args, "--decode-system", cfg.DecodeSystemName)
	}
	if cfg.BackendVersion != "" {
		args = append(args, "--backend-version", cfg.BackendVersion)
	}
	if cfg.Prefix > 0 {
		args = append(args, "--prefix", strconv.Itoa(cfg.Prefix))
	}
	if cfg.RequestLatency > 0 {
		args = append(args, "--request-latency", strconv.FormatFloat(cfg.RequestLatency, 'f', -1, 64))
	}

	// Extra tool-specific arguments
	for key, value := range cfg.ExtraArgs {
		args = append(args, fmt.Sprintf("--%s", key), value)
	}

	return args
}
