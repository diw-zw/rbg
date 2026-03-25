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

// Package plugin defines the generator plugin interface and implementations
// for the generate command. Each plugin is responsible for providing the
// container image and command used to run a configuration generation Job.
package plugin

import corev1 "k8s.io/api/core/v1"

// Plugin is the interface for a configuration generator plugin.
// A plugin provides the container spec (image + command) for a Kubernetes Job
// that runs the generation tool inside the cluster.
//
// The executor is responsible for:
//   - Creating the Job and mounting storage via the storage plugin
//   - Waiting for the Job to complete
//   - Downloading the output to local disk
//
// The plugin is responsible for:
//   - Providing the container image to use
//   - Building the command and arguments passed to the container
type Plugin interface {
	// Name returns the plugin name, e.g. "aiconfigurator" or "general".
	Name() string

	// Container returns the container spec for the generator Job.
	// The container's command, args, image, and resource requirements are set here.
	// Storage volume mounts are added by the executor via the storage plugin.
	Container(cfg *Config) (corev1.Container, error)
}

// Config holds all parameters needed by a generator plugin to build a Job container.
// It is constructed by the executor from the user-facing TaskConfig.
type Config struct {
	// ModelName is the model identifier, e.g. "Qwen/Qwen3-32B".
	ModelName string
	// SystemName is the hardware system name, e.g. "h200_sxm".
	SystemName string
	// DecodeSystemName is the hardware system used for decode workers in disagg mode.
	DecodeSystemName string
	// BackendName is the inference backend, e.g. "sglang".
	BackendName string
	// BackendVersion is an optional pinned backend version.
	BackendVersion string
	// TotalGPUs is the total number of GPUs available.
	TotalGPUs int
	// ISL is the input sequence length.
	ISL int
	// OSL is the output sequence length.
	OSL int
	// TTFT is the time-to-first-token SLA in ms.
	TTFT float64
	// TPOT is the time-per-output-token SLA in ms.
	TPOT float64
	// RequestLatency is an optional request latency SLA in ms.
	RequestLatency float64
	// Prefix is the prefix cache ratio (0-100).
	Prefix int
	// DatabaseMode is the aiconfigurator database mode, e.g. "SILICON".
	DatabaseMode string

	// ModelPath is the path to the model inside the container (under the storage mount).
	// Example: "/models/Qwen/Qwen3-32B"
	ModelPath string
	// OutputDir is the path inside the container where generated YAML files are written.
	// Example: "/models/.rbg/generate/Qwen3-32B"
	OutputDir string
	// Namespace is the Kubernetes namespace for the Job and generated resources.
	Namespace string
	// Image is the container image to use for the generator Job.
	// If empty, the plugin uses its default image.
	Image string

	// ExtraArgs holds any additional tool-specific arguments.
	ExtraArgs map[string]string
}
