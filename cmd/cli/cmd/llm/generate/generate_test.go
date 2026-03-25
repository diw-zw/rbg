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

package generate

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func validConfig() *TaskConfig {
	return &TaskConfig{
		ModelName:    "Qwen/Qwen3-32B",
		SystemName:   "h200_sxm",
		TotalGPUs:    8,
		BackendName:  BackendSGLang,
		ISL:          4000,
		OSL:          1000,
		TTFT:         1000,
		TPOT:         10,
		DatabaseMode: DatabaseModeSilicon,
		ExtraArgs:    make(map[string]string),
	}
}

// ---- validateConfig ----

func TestValidateConfig_ValidTTFTAndTPOT(t *testing.T) {
	cfg := validConfig()
	assert.NoError(t, validateConfig(cfg))
}

func TestValidateConfig_ValidRequestLatency(t *testing.T) {
	cfg := validConfig()
	cfg.TTFT = -1
	cfg.TPOT = -1
	cfg.RequestLatency = 500
	assert.NoError(t, validateConfig(cfg))
}

func TestValidateConfig_MissingModel(t *testing.T) {
	cfg := validConfig()
	cfg.ModelName = ""
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--model is required")
}

func TestValidateConfig_MissingSystem(t *testing.T) {
	cfg := validConfig()
	cfg.SystemName = ""
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--system is required")
}

func TestValidateConfig_ZeroTotalGPUs(t *testing.T) {
	cfg := validConfig()
	cfg.TotalGPUs = 0
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--total-gpus must be greater than 0")
}

func TestValidateConfig_MissingLatency(t *testing.T) {
	cfg := validConfig()
	cfg.TTFT = -1
	cfg.TPOT = -1
	cfg.RequestLatency = -1
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "latency parameters validation failed")
}

func TestValidateConfig_OnlyTTFTSet(t *testing.T) {
	cfg := validConfig()
	cfg.TTFT = 1000
	cfg.TPOT = -1
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "latency parameters validation failed")
}

func TestValidateConfig_InvalidBackend(t *testing.T) {
	cfg := validConfig()
	cfg.BackendName = "invalid-backend"
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid backend")
}

func TestValidateConfig_AllBackends(t *testing.T) {
	for _, backend := range []string{BackendSGLang, BackendVLLM, BackendTRTLLM} {
		cfg := validConfig()
		cfg.BackendName = backend
		assert.NoError(t, validateConfig(cfg), "backend %s should be valid", backend)
	}
}

func TestValidateConfig_InvalidSystem(t *testing.T) {
	cfg := validConfig()
	cfg.SystemName = "unknown_gpu"
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid system")
}

func TestValidateConfig_ValidSystems(t *testing.T) {
	for _, sys := range []string{"h100_sxm", "a100_sxm", "b200_sxm", "gb200_sxm", "l40s", "h200_sxm"} {
		cfg := validConfig()
		cfg.SystemName = sys
		assert.NoError(t, validateConfig(cfg), "system %s should be valid", sys)
	}
}

func TestValidateConfig_InvalidDatabaseMode(t *testing.T) {
	cfg := validConfig()
	cfg.DatabaseMode = "INVALID"
	err := validateConfig(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid database-mode")
}

func TestValidateConfig_AllDatabaseModes(t *testing.T) {
	for _, mode := range []string{DatabaseModeSilicon, DatabaseModeHybrid, DatabaseModeEmpirical, DatabaseModeSOL} {
		cfg := validConfig()
		cfg.DatabaseMode = mode
		assert.NoError(t, validateConfig(cfg), "database mode %s should be valid", mode)
	}
}

// ---- GetModelBaseName ----

func TestGetModelBaseName_HuggingFacePath(t *testing.T) {
	assert.Equal(t, "Qwen3-32B", GetModelBaseName("Qwen/Qwen3-32B"))
}

func TestGetModelBaseName_SimpleName(t *testing.T) {
	assert.Equal(t, "llama3", GetModelBaseName("llama3"))
}

func TestGetModelBaseName_TrailingSlash(t *testing.T) {
	// path.Clean strips trailing slash before filepath.Base
	result := GetModelBaseName("models/llama3/")
	assert.Equal(t, "llama3", result)
}

func TestGetModelBaseName_EmptyString(t *testing.T) {
	result := GetModelBaseName("")
	assert.Equal(t, "", result)
}
