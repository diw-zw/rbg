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

package svc

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	llmmeta "sigs.k8s.io/rbgs/cmd/cli/cmd/llm/svc/metadata"
)

// TestMain sets up an isolated test environment for all tests in this package.
// We set RBG_MODEL_CONFIG to a non-existent path to prevent loading user models,
// ensuring tests only use the built-in models.yaml definitions.
func TestMain(m *testing.M) {
	// Set RBG_MODEL_CONFIG to a non-existent path to skip user model loading
	// This ensures tests use only built-in models, making them deterministic
	_ = os.Setenv("RBG_MODEL_CONFIG", "/nonexistent/path/for/testing")
	os.Exit(m.Run())
}

// --- newRunCmd: command metadata ---

func TestNewRunCmd_UseAndShort(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)
	cmd := newRunCmd(cf)
	assert.Equal(t, "run <name> <model-id> [flags]", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

func TestNewRunCmd_ExactlyTwoArgs(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)
	cmd := newRunCmd(cf)
	// no args should produce an error
	err := cmd.Args(cmd, []string{})
	require.Error(t, err)

	// one arg should also error
	err = cmd.Args(cmd, []string{"my-qwen"})
	require.Error(t, err)

	// three args should also error
	err = cmd.Args(cmd, []string{"my-qwen", "Qwen/Qwen3.5-0.8B", "extra"})
	require.Error(t, err)

	// exactly two args is fine
	err = cmd.Args(cmd, []string{"my-qwen", "Qwen/Qwen3.5-0.8B"})
	require.NoError(t, err)
}

// --- newRunCmd: flags exist with expected defaults ---

func TestNewRunCmd_FlagDefaults(t *testing.T) {
	cf := genericclioptions.NewConfigFlags(true)
	cmd := newRunCmd(cf)

	// --name flag should not exist (now positional arg)
	nameFlag := cmd.Flags().Lookup("name")
	assert.Nil(t, nameFlag)

	// --mode default is empty (first mode in model config is used)
	modeFlag := cmd.Flags().Lookup("mode")
	require.NotNil(t, modeFlag)
	assert.Equal(t, "", modeFlag.DefValue)

	// --replicas default is 1
	replicasFlag := cmd.Flags().Lookup("replicas")
	require.NotNil(t, replicasFlag)
	assert.Equal(t, "1", replicasFlag.DefValue)

	// --revision default is "main"
	revFlag := cmd.Flags().Lookup("revision")
	require.NotNil(t, revFlag)
	assert.Equal(t, "main", revFlag.DefValue)

	// --storage default is empty
	storageFlag := cmd.Flags().Lookup("storage")
	require.NotNil(t, storageFlag)
	assert.Equal(t, "", storageFlag.DefValue)

	// --engine default is empty
	engineFlag := cmd.Flags().Lookup("engine")
	require.NotNil(t, engineFlag)
	assert.Equal(t, "", engineFlag.DefValue)

	// --dry-run default is false
	dryRunFlag := cmd.Flags().Lookup("dry-run")
	require.NotNil(t, dryRunFlag)
	assert.Equal(t, "false", dryRunFlag.DefValue)

	// --env and --arg are StringArray, default empty
	envFlag := cmd.Flags().Lookup("env")
	require.NotNil(t, envFlag)

	argFlag := cmd.Flags().Lookup("arg")
	require.NotNil(t, argFlag)
}

// --- env-var parsing logic (mirrors run.go's inline SplitN logic) ---
// run.go: parts := strings.SplitN(env, "=", 2)
// We test the same logic directly.

func splitEnvVarTestHelper(env string) []string {
	return strings.SplitN(env, "=", 2)
}

func TestRunEnvVarParsing_ValidKeyValue(t *testing.T) {
	parts := splitEnvVarTestHelper("FOO=bar")
	require.Len(t, parts, 2)
	assert.Equal(t, "FOO", parts[0])
	assert.Equal(t, "bar", parts[1])
}

func TestRunEnvVarParsing_ValueWithEquals(t *testing.T) {
	// Value itself contains "=" — only the first "=" is the separator
	parts := splitEnvVarTestHelper("KEY=val=ue")
	require.Len(t, parts, 2)
	assert.Equal(t, "KEY", parts[0])
	assert.Equal(t, "val=ue", parts[1])
}

func TestRunEnvVarParsing_NoEqualsSign(t *testing.T) {
	// SplitN with n=2 returns single element when no separator
	parts := splitEnvVarTestHelper("NOEQUALS")
	assert.Len(t, parts, 1)
}

func TestRunEnvVarParsing_EmptyValue(t *testing.T) {
	parts := splitEnvVarTestHelper("EMPTY=")
	require.Len(t, parts, 2)
	assert.Equal(t, "EMPTY", parts[0])
	assert.Equal(t, "", parts[1])
}

// --- generateRBG ---

func TestGenerateRBG_DefaultMode_VLLMEngine(t *testing.T) {
	// Qwen/Qwen3.5-0.8B standard mode uses vllm with port 8000
	rbg, metadata, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Revision: "main",
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.NoError(t, err)
	assert.Equal(t, "my-svc", rbg.Name)
	assert.Equal(t, "default", rbg.Namespace)

	// Check returned metadata
	assert.Equal(t, "vllm", metadata.Engine)
	assert.Equal(t, "standard", metadata.Mode)
	assert.Equal(t, int32(8000), metadata.Port)

	// Check metadata annotation
	var annotationMetadata llmmeta.RunMetadata
	err = json.Unmarshal([]byte(rbg.Annotations[llmmeta.RunCommandMetadataAnnotationKey]), &annotationMetadata)
	require.NoError(t, err)
	assert.Equal(t, "vllm", annotationMetadata.Engine)
	assert.Equal(t, "standard", annotationMetadata.Mode)
	assert.Equal(t, int32(8000), annotationMetadata.Port)
}

func TestGenerateRBG_LatencyMode_SGLangEngine(t *testing.T) {
	// Qwen/Qwen3.5-0.8B latency mode uses sglang with port 30000
	rbg, metadata, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Mode:     "latency",
		Revision: "main",
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.NoError(t, err)

	// Check returned metadata
	assert.Equal(t, "sglang", metadata.Engine)
	assert.Equal(t, "latency", metadata.Mode)
	assert.Equal(t, int32(30000), metadata.Port)

	// Check metadata annotation
	var annotationMetadata llmmeta.RunMetadata
	err = json.Unmarshal([]byte(rbg.Annotations[llmmeta.RunCommandMetadataAnnotationKey]), &annotationMetadata)
	require.NoError(t, err)
	assert.Equal(t, "sglang", annotationMetadata.Engine)
	assert.Equal(t, "latency", annotationMetadata.Mode)
	assert.Equal(t, int32(30000), annotationMetadata.Port)
}

func TestGenerateRBG_EngineOverride(t *testing.T) {
	// Engine flag overrides the mode's default engine
	rbg, metadata, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Engine:   "sglang",
		Revision: "main",
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.NoError(t, err)

	// Check returned metadata
	assert.Equal(t, "sglang", metadata.Engine)

	// Check metadata annotation
	var annotationMetadata llmmeta.RunMetadata
	err = json.Unmarshal([]byte(rbg.Annotations[llmmeta.RunCommandMetadataAnnotationKey]), &annotationMetadata)
	require.NoError(t, err)
	assert.Equal(t, "sglang", annotationMetadata.Engine)
}

func TestGenerateRBG_EnvVarInjection(t *testing.T) {
	rbg, _, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Revision: "main",
		EnvVars:  []string{"MY_KEY=my_value"},
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.NoError(t, err)

	podTemplate := getPodTemplateFromPattern(&rbg.Spec.Roles[0].Pattern)
	envMap := map[string]string{}
	for _, e := range podTemplate.Spec.Containers[0].Env {
		envMap[e.Name] = e.Value
	}
	assert.Equal(t, "my_value", envMap["MY_KEY"])
}

func TestGenerateRBG_InvalidEnvVar(t *testing.T) {
	_, _, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Revision: "main",
		EnvVars:  []string{"NOEQUALSSIGN"},
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid environment variable format")
}

func TestGenerateRBG_UnknownModel(t *testing.T) {
	// Unknown model should return error when no wildcard config exists
	_, _, err := generateRBG("my-svc", "unknown/unknown-model", "default", RunParams{
		Revision: "main",
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no configuration found for model")
}

func TestGenerateRBG_UnknownEngine_Errors(t *testing.T) {
	_, _, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Engine:   "nonexistent-engine",
		Revision: "main",
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown engine type")
}

func TestGenerateRBG_AdditionalArgs(t *testing.T) {
	rbg, _, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Revision: "main",
		ArgsList: []string{"--custom-flag", "value"},
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.NoError(t, err)

	podTemplate := getPodTemplateFromPattern(&rbg.Spec.Roles[0].Pattern)
	args := podTemplate.Spec.Containers[0].Args
	assert.Contains(t, args, "--custom-flag")
	assert.Contains(t, args, "value")
}

func TestGenerateRBG_FallbackModelPath(t *testing.T) {
	// Without storage config, model path uses the /model/ fallback
	rbg, _, err := generateRBG("my-svc", "Qwen/Qwen3.5-0.8B", "default", RunParams{
		Revision: "main",
		Replicas: 1,
		DryRun:   true,
	}, nil, nil)
	require.NoError(t, err)

	podTemplate := getPodTemplateFromPattern(&rbg.Spec.Roles[0].Pattern)
	args := podTemplate.Spec.Containers[0].Args
	var modelPathArg string
	for i, a := range args {
		if a == "--model" && i+1 < len(args) {
			modelPathArg = args[i+1]
			break
		}
	}
	assert.True(t, strings.HasPrefix(modelPathArg, "/model/"), "expected fallback model path, got: %s", modelPathArg)
}
