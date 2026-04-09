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

package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"

	workloadsv1alpha2 "sigs.k8s.io/rbgs/api/workloads/v1alpha2"
	llmmeta "sigs.k8s.io/rbgs/cmd/cli/cmd/llm/metadata"
	runpkg "sigs.k8s.io/rbgs/cmd/cli/cmd/llm/run"
	cliconfig "sigs.k8s.io/rbgs/cmd/cli/config"
	engineplugin "sigs.k8s.io/rbgs/cmd/cli/plugin/engine"
	storageplugin "sigs.k8s.io/rbgs/cmd/cli/plugin/storage"
	"sigs.k8s.io/rbgs/cmd/cli/util"
)

// resolveEngine resolves the engine configuration.
// First tries to get from user config, then falls back to registered plugin with defaults.
func resolveEngine(engineType string, cfg *cliconfig.Config) (*cliconfig.EngineConfig, error) {
	// 1. Try to get from user config (if available)
	if cfg != nil {
		if engineCfg, err := cfg.GetEngine(engineType); err == nil {
			return engineCfg, nil
		}
	}

	// 2. Check if it's a registered plugin type
	if !engineplugin.IsRegistered(engineType) {
		return nil, fmt.Errorf("unknown engine type '%s'", engineType)
	}

	// 3. Use default (empty config) - plugin will use its built-in defaults
	fmt.Printf("INFO: Using default configuration for engine '%s'. Run 'kubectl rbg llm config add-engine %s' to customize.\n", engineType, engineType)
	return &cliconfig.EngineConfig{
		Type:   engineType,
		Config: map[string]interface{}{},
	}, nil
}

// RunParams holds all flag values supplied to the run command.
type RunParams struct {
	Mode     string
	Engine   string
	Storage  string
	Revision string
	EnvVars  []string
	ArgsList []string
	DryRun   bool
	Replicas int32
}

// modeConfigResult holds the result of mode config resolution.
type modeConfigResult struct {
	modelCfg     *runpkg.ModelConfig
	modeCfg      *runpkg.ModeConfig
	enginePlugin engineplugin.Plugin
	engineType   string
}

// resolveModeConfig resolves model, mode, and engine configuration.
func resolveModeConfig(modelID string, p RunParams, userCfg *cliconfig.Config) (*modeConfigResult, error) {
	models, err := runpkg.LoadAllModels()
	if err != nil {
		return nil, fmt.Errorf("failed to load model configs: %w", err)
	}
	modelCfg, err := runpkg.FindModelConfig(models, modelID)
	if err != nil {
		return nil, err
	}
	modeCfg, err := runpkg.FindModeConfig(modelCfg, p.Mode)
	if err != nil {
		return nil, err
	}

	engineType := modeCfg.Engine
	if p.Engine != "" {
		engineType = p.Engine
	}
	engineCfg, err := resolveEngine(engineType, userCfg)
	if err != nil {
		return nil, err
	}
	enginePlugin, err := engineplugin.Get(engineCfg.Type, engineCfg.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize engine %q: %w", engineType, err)
	}

	return &modeConfigResult{
		modelCfg:     modelCfg,
		modeCfg:      modeCfg,
		enginePlugin: enginePlugin,
		engineType:   engineType,
	}, nil
}

// storageResult holds the result of storage resolution.
type storageResult struct {
	modelPath     string
	storagePlugin storageplugin.Plugin
	storageName   string
}

// resolveStorageAndModelPath resolves storage plugin and model path.
func resolveStorageAndModelPath(modelID string, p RunParams, userCfg *cliconfig.Config) *storageResult {
	var modelPath string
	var storagePlugin storageplugin.Plugin
	var storageName string

	if userCfg != nil {
		storageName = userCfg.CurrentStorage
		if p.Storage != "" {
			storageName = p.Storage
		}
		if storageName != "" {
			if storageCfg, err := userCfg.GetStorage(storageName); err == nil {
				if sp, err := storageplugin.Get(storageCfg.Type, storageCfg.Config); err == nil {
					storagePlugin = sp
					modelPath = filepath.Join(sp.MountPath(), sanitizeModelID(modelID), sanitizeModelID(p.Revision))
				}
			}
		}
	}
	if modelPath == "" {
		modelPath = "/model/" + sanitizeModelID(modelID) + "/" + sanitizeModelID(p.Revision)
	}

	return &storageResult{
		modelPath:     modelPath,
		storagePlugin: storagePlugin,
		storageName:   storageName,
	}
}

// buildGenerateOptions builds GenerateOptions from mode config and run params.
func buildGenerateOptions(name, modelID, modelPath string, modeCfg *runpkg.ModeConfig, p RunParams) (engineplugin.GenerateOptions, error) {
	distributedSize := int32(0)
	if modeCfg.Distributed != nil && modeCfg.Distributed.Size > 1 {
		distributedSize = modeCfg.Distributed.Size
	}

	envVars := make([]corev1.EnvVar, len(modeCfg.Env))
	copy(envVars, modeCfg.Env)
	for _, ev := range p.EnvVars {
		parts := strings.SplitN(ev, "=", 2)
		if len(parts) != 2 {
			return engineplugin.GenerateOptions{}, fmt.Errorf("invalid environment variable format: %q, expected KEY=VALUE", ev)
		}
		envVars = append(envVars, corev1.EnvVar{Name: parts[0], Value: parts[1]})
	}

	var resources corev1.ResourceRequirements
	if len(modeCfg.Resources) > 0 {
		requests := corev1.ResourceList{}
		limits := corev1.ResourceList{}
		for k, v := range modeCfg.Resources {
			requests[k] = v
			limits[k] = v
		}
		resources.Requests = requests
		resources.Limits = limits
	}

	return engineplugin.GenerateOptions{
		Name:            name,
		ModelID:         modelID,
		ModelPath:       modelPath,
		Image:           modeCfg.Image,
		Args:            append(modeCfg.Args, p.ArgsList...),
		Env:             envVars,
		Resources:       resources,
		DistributedSize: distributedSize,
		ShmSize:         modeCfg.ShmSize,
	}, nil
}

// assembleRBG assembles a RoleBasedGroup from pattern and metadata.
func assembleRBG(name, namespace string, pattern *workloadsv1alpha2.Pattern, metadata llmmeta.RunMetadata, replicas int32) *workloadsv1alpha2.RoleBasedGroup {
	podTemplate := getPodTemplateFromPattern(pattern)
	if podTemplate.ObjectMeta.Labels == nil {
		podTemplate.ObjectMeta.Labels = make(map[string]string)
	}
	podTemplate.ObjectMeta.Labels[llmmeta.RunCommandSourceLabelKey] = llmmeta.RunCommandSourceLabelValue

	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		klog.V(1).Infof("failed to marshal run metadata: %v", err)
	}

	roleSpec := workloadsv1alpha2.RoleSpec{
		Name:     "inference",
		Replicas: &replicas,
		Pattern:  *pattern,
		// TODO: Remove workload field after PR #261
		Workload: workloadsv1alpha2.WorkloadSpec{
			APIVersion: "workloads.x-k8s.io/v1alpha2",
			Kind:       "RoleInstanceSet",
		},
	}

	return &workloadsv1alpha2.RoleBasedGroup{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "workloads.x-k8s.io/v1alpha2",
			Kind:       "RoleBasedGroup",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				llmmeta.RunCommandSourceLabelKey: llmmeta.RunCommandSourceLabelValue,
			},
			Annotations: map[string]string{
				llmmeta.RunCommandMetadataAnnotationKey: string(metadataJSON),
			},
		},
		Spec: workloadsv1alpha2.RoleBasedGroupSpec{
			Roles: []workloadsv1alpha2.RoleSpec{roleSpec},
		},
	}
}

// generateRBG generates a RoleBasedGroup and prints summary information.
// It performs: model config resolution → pattern generation → storage mounting → RBG assembly.
func generateRBG(name, modelID, namespace string, p RunParams, userCfg *cliconfig.Config, cf *genericclioptions.ConfigFlags) (*workloadsv1alpha2.RoleBasedGroup, error) {
	// 1. Resolve model/mode/engine config
	modeRes, err := resolveModeConfig(modelID, p, userCfg)
	if err != nil {
		return nil, err
	}

	// 2. Resolve storage and model path
	storageRes := resolveStorageAndModelPath(modelID, p, userCfg)

	// 3. Build GenerateOptions
	opts, err := buildGenerateOptions(name, modelID, storageRes.modelPath, modeRes.modeCfg, p)
	if err != nil {
		return nil, err
	}

	// 4. Generate pattern
	pattern, err := modeRes.enginePlugin.GeneratePattern(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to generate engine pattern: %w", err)
	}
	podTemplate := getPodTemplateFromPattern(pattern)
	if podTemplate == nil || len(podTemplate.Spec.Containers) == 0 {
		return nil, fmt.Errorf("engine %q generated pattern with no containers", modeRes.engineType)
	}

	// 5. Mount storage
	if storageRes.storagePlugin != nil && storageRes.storageName != "" {
		mountOpts := storageplugin.MountOptions{
			StorageName: storageRes.storageName,
			Namespace:   namespace,
			DryRun:      p.DryRun,
		}
		if !p.DryRun {
			c, err := util.GetControllerRuntimeClient(cf)
			if err != nil {
				return nil, fmt.Errorf("failed to create controller client: %w", err)
			}
			mountOpts.Client = c
		}
		if err := storageRes.storagePlugin.MountStorage(podTemplate, mountOpts); err != nil {
			return nil, fmt.Errorf("failed to mount storage: %w", err)
		}
	}

	// 6. Extract port and build metadata
	var resolvedPort int32
	for _, cp := range podTemplate.Spec.Containers[0].Ports {
		if cp.Name == "http" {
			resolvedPort = cp.ContainerPort
			break
		}
	}
	metadata := llmmeta.RunMetadata{
		ModelID:  modelID,
		Engine:   modeRes.engineType,
		Mode:     modeRes.modeCfg.Name,
		Revision: p.Revision,
		Port:     resolvedPort,
	}

	// 7. Assemble RBG
	rbg := assembleRBG(name, namespace, pattern, metadata, p.Replicas)

	// 8. Print summary
	fmt.Println("# Generated RoleBasedGroup for Model Serving")
	fmt.Printf("# Name:      %s\n", name)
	fmt.Printf("# Namespace: %s\n", namespace)
	fmt.Printf("# Model:     %s\n", modelID)
	fmt.Printf("# Revision:  %s\n", p.Revision)
	fmt.Printf("# Mode:      %s\n", modeRes.modeCfg.Name)
	fmt.Printf("# Engine:    %s\n", modeRes.engineType)
	fmt.Printf("# Replicas:  %d\n", p.Replicas)
	fmt.Println("#")

	return rbg, nil
}

// getPodTemplateFromPattern extracts the pod template from a Pattern
func getPodTemplateFromPattern(pattern *workloadsv1alpha2.Pattern) *corev1.PodTemplateSpec {
	if pattern == nil {
		return nil
	}
	if pattern.StandalonePattern != nil && pattern.StandalonePattern.Template != nil {
		return pattern.StandalonePattern.Template
	}
	if pattern.LeaderWorkerPattern != nil && pattern.LeaderWorkerPattern.Template != nil {
		return pattern.LeaderWorkerPattern.Template
	}
	return nil
}

// createRBG creates a v1alpha2 RoleBasedGroup in Kubernetes
func createRBG(ctx context.Context, rbg *workloadsv1alpha2.RoleBasedGroup, cf *genericclioptions.ConfigFlags) error {
	client, err := util.GetRBGClient(cf)
	if err != nil {
		return fmt.Errorf("failed to create RBG client: %w", err)
	}

	_, err = client.WorkloadsV1alpha2().RoleBasedGroups(rbg.Namespace).Create(ctx, rbg, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create RoleBasedGroup: %w", err)
	}

	return nil
}

func newRunCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	var (
		replicas int32
		mode     string
		engine   string
		envVars  []string
		argsList []string
		storage  string
		revision string
		dryRun   bool
	)

	cmd := &cobra.Command{
		Use:   "run <name> <model-id> [flags]",
		Short: "Run a model as an inference service",
		Long: `Deploy a model as an inference service on Kubernetes using RoleBasedGroup.

This command creates a RoleBasedGroup resource that deploys an LLM model for inference.
It supports various inference engines (vLLM, SGLang) and deployment modes optimized
for different use cases (latency, throughput, etc.).

The command will:
  1. Load the model configuration from the built-in models database
  2. Generate a pod template using the specified inference engine
  3. Create a RoleBasedGroup resource in the cluster

Prerequisites:
  - The model should be available in storage (use 'kubectl rbg llm pull' first)
  - Storage must be configured (use 'kubectl rbg llm config add-storage')

Examples:
  # Quick start with default config
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B

  # Use a specific mode
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B --mode throughput

  # Override engine
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B --mode custom --engine sglang

  # Run with multiple replicas
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B --replicas 3

  # Dry run to preview the generated configuration
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B --dry-run`,
		Example: `  # Quick start with default config
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B

  # Use a specific mode
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B --mode throughput

  # Override engine
  kubectl rbg llm run my-qwen Qwen/Qwen3.5-0.8B --mode custom --engine sglang`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			modelID := args[1]
			namespace := util.GetNamespace(cf)

			// Load user config (best-effort — optional for engine and storage resolution)
			userCfg, cfgErr := cliconfig.Load()
			if cfgErr != nil {
				klog.V(1).Infof("Warning: failed to load user config: %v", cfgErr)
			}

			// Generate RBG (includes config resolution, pattern generation, storage mounting)
			rbg, err := generateRBG(name, modelID, namespace, RunParams{
				Mode:     mode,
				Engine:   engine,
				Storage:  storage,
				Revision: revision,
				EnvVars:  envVars,
				ArgsList: argsList,
				DryRun:   dryRun,
				Replicas: replicas,
			}, userCfg, cf)
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Println("# DRY RUN: No workload will be created")
				fmt.Println()
				return printRBG(rbg)
			}

			// Create the RoleBasedGroup workload
			ctx := context.Background()
			if err := createRBG(ctx, rbg, cf); err != nil {
				klog.ErrorS(err, "Failed to create RoleBasedGroup")
				return err
			}

			fmt.Printf("✓ RoleBasedGroup '%s' created successfully in namespace '%s'\n", name, namespace)
			return nil
		},
	}

	cmd.Flags().Int32Var(&replicas, "replicas", 1, "Number of replicas")
	cmd.Flags().StringVar(&mode, "mode", "", "Run mode (default: first mode in model config)")
	cmd.Flags().StringVar(&engine, "engine", "", "Inference engine override: vllm, sglang (default: from mode config)")
	cmd.Flags().StringArrayVar(&envVars, "env", nil, "Environment variables (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&argsList, "arg", nil, "Additional arguments for the engine")
	cmd.Flags().StringVar(&storage, "storage", "", "Storage to use (overrides default)")
	cmd.Flags().StringVar(&revision, "revision", "main", "Model revision")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Print the generated template without creating the workload")

	return cmd
}
