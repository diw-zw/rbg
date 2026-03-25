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
	"testing"

	"github.com/stretchr/testify/assert"
)

func validAICfg() *Config {
	return &Config{
		ModelName:    "Qwen/Qwen3-32B",
		SystemName:   "h200_sxm",
		TotalGPUs:    8,
		BackendName:  "sglang",
		ISL:          4000,
		OSL:          1000,
		TTFT:         1000,
		TPOT:         10,
		DatabaseMode: "SILICON",
		ModelPath:    "/models/Qwen/Qwen3-32B",
		OutputDir:    "/models/.rbg/generate/qwen3-32b",
		Namespace:    "default",
		ExtraArgs:    make(map[string]string),
	}
}

// ---- AIConfiguratorPlugin.Name ----

func TestAIConfiguratorPlugin_Name(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	assert.Equal(t, AIConfiguratorPluginName, p.Name())
}

// ---- AIConfiguratorPlugin.Container ----

func TestAIConfiguratorPlugin_Container_DefaultImage(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()

	c, err := p.Container(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "generate", c.Name)
	assert.Equal(t, DefaultAIConfiguratorImage, c.Image)
	assert.Equal(t, []string{generateRBGYAMLBin}, c.Command)
}

func TestAIConfiguratorPlugin_Container_CustomImage(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.Image = "my-registry/aiconfigurator:v1"

	c, err := p.Container(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "my-registry/aiconfigurator:v1", c.Image)
}

func TestAIConfiguratorPlugin_Container_MissingModelPath(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.ModelPath = ""

	_, err := p.Container(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "ModelPath is required")
}

func TestAIConfiguratorPlugin_Container_MissingOutputDir(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.OutputDir = ""

	_, err := p.Container(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OutputDir is required")
}

// ---- buildArgs ----

func TestAIConfiguratorPlugin_BuildArgs_RequiredFlags(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	args := p.buildArgs(cfg)

	assertContainsFlag(t, args, "--model", "Qwen/Qwen3-32B")
	assertContainsFlag(t, args, "--system", "h200_sxm")
	assertContainsFlag(t, args, "--total-gpus", "8")
	assertContainsFlag(t, args, "--backend", "sglang")
	assertContainsFlag(t, args, "--isl", "4000")
	assertContainsFlag(t, args, "--osl", "1000")
	assertContainsFlag(t, args, "--ttft", "1000")
	assertContainsFlag(t, args, "--tpot", "10")
	assertContainsFlag(t, args, "--model-path", "/models/Qwen/Qwen3-32B")
	assertContainsFlag(t, args, "--output-dir", "/models/.rbg/generate/qwen3-32b")
	assertContainsFlag(t, args, "--namespace", "default")
	assertContainsFlag(t, args, "--database-mode", "SILICON")
}

func TestAIConfiguratorPlugin_BuildArgs_OptionalDecodeSystem(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.DecodeSystemName = "h100_sxm"

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--decode-system", "h100_sxm")
}

func TestAIConfiguratorPlugin_BuildArgs_SameDecodeSystem(t *testing.T) {
	// If decode-system == system, flag should NOT be added
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.DecodeSystemName = cfg.SystemName

	args := p.buildArgs(cfg)
	assert.NotContains(t, args, "--decode-system")
}

func TestAIConfiguratorPlugin_BuildArgs_OptionalBackendVersion(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.BackendVersion = "0.4.0"

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--backend-version", "0.4.0")
}

func TestAIConfiguratorPlugin_BuildArgs_OptionalPrefix(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.Prefix = 50

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--prefix", "50")
}

func TestAIConfiguratorPlugin_BuildArgs_ZeroPrefixOmitted(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.Prefix = 0

	args := p.buildArgs(cfg)
	assert.NotContains(t, args, "--prefix")
}

func TestAIConfiguratorPlugin_BuildArgs_RequestLatency(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.RequestLatency = 500

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--request-latency", "500")
}

func TestAIConfiguratorPlugin_BuildArgs_ExtraArgs(t *testing.T) {
	p := &AIConfiguratorPlugin{}
	cfg := validAICfg()
	cfg.ExtraArgs = map[string]string{"custom-flag": "custom-value"}

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--custom-flag", "custom-value")
}

// assertContainsFlag checks that args contains "--flag" immediately followed by value.
func assertContainsFlag(t *testing.T, args []string, flag, value string) {
	t.Helper()
	for i, arg := range args {
		if arg == flag && i+1 < len(args) && args[i+1] == value {
			return
		}
	}
	t.Errorf("args %v does not contain flag %s with value %s", args, flag, value)
}
