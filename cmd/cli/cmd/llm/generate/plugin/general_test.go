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

// ---- GeneralPlugin.Name ----

func TestGeneralPlugin_Name(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	assert.Equal(t, GeneralPluginName, p.Name())
}

// ---- GeneralPlugin.Container ----

func TestGeneralPlugin_Container_Basic(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	cfg := &Config{
		ModelName: "llama3",
		Image:     "my-registry/mytool:latest",
		ExtraArgs: make(map[string]string),
	}

	c, err := p.Container(cfg)
	assert.NoError(t, err)
	assert.Equal(t, "generate", c.Name)
	assert.Equal(t, "my-registry/mytool:latest", c.Image)
	assert.Equal(t, []string{"mytool"}, c.Command)
}

func TestGeneralPlugin_Container_MissingToolName(t *testing.T) {
	p := NewGeneralPlugin("")
	cfg := &Config{
		Image:     "my-registry/mytool:latest",
		ExtraArgs: make(map[string]string),
	}

	_, err := p.Container(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "toolName is required")
}

func TestGeneralPlugin_Container_MissingImage(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	cfg := &Config{
		ModelName: "llama3",
		ExtraArgs: make(map[string]string),
	}

	_, err := p.Container(cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "Image is required")
}

// ---- GeneralPlugin.buildArgs ----

func TestGeneralPlugin_BuildArgs_OnlyNonZero(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	cfg := &Config{
		ModelName:   "llama3",
		SystemName:  "h200_sxm",
		TotalGPUs:   4,
		BackendName: "vllm",
		ISL:         2048,
		OSL:         512,
		ModelPath:   "/models/llama3",
		OutputDir:   "/models/.rbg/generate/llama3",
		ExtraArgs:   make(map[string]string),
	}

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--model", "llama3")
	assertContainsFlag(t, args, "--system", "h200_sxm")
	assertContainsFlag(t, args, "--total-gpus", "4")
	assertContainsFlag(t, args, "--backend", "vllm")
	assertContainsFlag(t, args, "--isl", "2048")
	assertContainsFlag(t, args, "--osl", "512")
	assertContainsFlag(t, args, "--model-path", "/models/llama3")
	assertContainsFlag(t, args, "--output-dir", "/models/.rbg/generate/llama3")
}

func TestGeneralPlugin_BuildArgs_ZeroFieldsOmitted(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	cfg := &Config{
		// TotalGPUs = 0, ISL = 0, OSL = 0 → should be omitted
		ModelName: "llama3",
		ExtraArgs: make(map[string]string),
	}

	args := p.buildArgs(cfg)
	assert.NotContains(t, args, "--total-gpus")
	assert.NotContains(t, args, "--isl")
	assert.NotContains(t, args, "--osl")
}

func TestGeneralPlugin_BuildArgs_ExtraArgs(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	cfg := &Config{
		Image:     "img",
		ExtraArgs: map[string]string{"custom": "val"},
	}

	args := p.buildArgs(cfg)
	assertContainsFlag(t, args, "--custom", "val")
}

func TestGeneralPlugin_BuildArgs_EmptyConfig(t *testing.T) {
	p := NewGeneralPlugin("mytool")
	cfg := &Config{
		ExtraArgs: make(map[string]string),
	}

	args := p.buildArgs(cfg)
	// No required fields → empty or minimal args
	assert.NotContains(t, args, "--model")
	assert.NotContains(t, args, "--system")
}
