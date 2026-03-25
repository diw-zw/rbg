/*
Copyright 2025.

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

package generate

// Configurator tool constants
const (
	AIConfigurator = "aiconfigurator"
)

// Backend constants
const (
	BackendSGLang = "sglang"
	BackendVLLM   = "vllm"
	BackendTRTLLM = "trtllm"
)

// Database mode constants
const (
	DatabaseModeSilicon   = "SILICON"
	DatabaseModeHybrid    = "HYBRID"
	DatabaseModeEmpirical = "EMPIRICAL"
	DatabaseModeSOL       = "SOL"
)

// TaskConfig holds the configuration for model deployment recommendation
type TaskConfig struct {
	ModelName        string
	SystemName       string
	DecodeSystemName string
	BackendName      string
	BackendVersion   string
	ISL              int
	OSL              int
	Prefix           int
	TTFT             float64
	TPOT             float64
	RequestLatency   float64
	TotalGPUs        int
	DatabaseMode     string
	SaveDir          string
	ConfiguratorTool string            // Name of the configurator tool to use (default: aiconfigurator)
	ExtraArgs        map[string]string // Additional unrecognized arguments
}
