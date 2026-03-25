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
	"context"
	"fmt"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	genericclioptions "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/klog/v2"
	"sigs.k8s.io/rbgs/cmd/cli/cmd/llm/generate/plugin"
	"sigs.k8s.io/rbgs/cmd/cli/config"
	storageplugin "sigs.k8s.io/rbgs/cmd/cli/plugin/storage"
	"sigs.k8s.io/rbgs/cmd/cli/util"
)

// NewGenerateCmd creates the generate command
func NewGenerateCmd(cf *genericclioptions.ConfigFlags) *cobra.Command {
	config := &TaskConfig{
		// Set defaults
		BackendName:      BackendSGLang,
		ISL:              4000,
		OSL:              1000,
		Prefix:           0,
		TTFT:             -1,
		TPOT:             -1,
		RequestLatency:   -1,
		DatabaseMode:     DatabaseModeSilicon,
		SaveDir:          "",
		ConfiguratorTool: AIConfigurator,
		ExtraArgs:        make(map[string]string),
	}

	var (
		storage      string
		image        string
		skipDownload bool
		silence      bool
	)

	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate optimized RBG deployment configurations using AI Configurator",
		Long: `The generate command runs a Kubernetes Pod that uses AI Configurator to generate
optimized deployment configurations for AI model serving. The model must already
exist in the configured storage. Generated YAML files are downloaded to --save-dir.

Example:
  kubectl-rbg llm generate --model Qwen/Qwen3-32B --system h200_sxm --total-gpus 8 \\
    --backend sglang --isl 4000 --osl 1000 --ttft 1000 --tpot 10

This will:
  1. Create a Kubernetes Pod with the aiconfigurator image
  2. Mount the configured storage into the Pod container
  3. Run aiconfigurator and render RBG deployment YAMLs inside the container
  4. Download the generated YAML files to --save-dir
  5. Delete the Pod after download completes (or on interrupt)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cf, config, storage, image, skipDownload, silence)
		},
	}

	cmd.Flags().StringVar(&config.ConfiguratorTool, "configurator-tool", AIConfigurator, "Configurator tool to use for generating deployment configs")

	// Core required parameters
	cmd.Flags().StringVar(&config.ModelName, "model", "", "Model name (required)")
	cmd.Flags().StringVar(&config.SystemName, "system", "", "GPU system type (required)")
	cmd.Flags().IntVar(&config.TotalGPUs, "total-gpus", 0, "Total number of GPUs for deployment (required)")
	cmd.Flags().IntVar(&config.ISL, "isl", 4000, "Input sequence length")
	cmd.Flags().IntVar(&config.OSL, "osl", 1000, "Output sequence length")

	cmd.Flags().Float64Var(&config.TTFT, "ttft", -1, "Time to first token in milliseconds")
	cmd.Flags().Float64Var(&config.TPOT, "tpot", -1, "Time per output token in milliseconds")
	cmd.Flags().Float64Var(&config.RequestLatency, "request-latency", -1, "End-to-end request latency target in milliseconds (alternative to --ttft and --tpot)")

	// Core optional parameters
	cmd.Flags().StringVar(&config.DecodeSystemName, "decode-system", "", "GPU system for decode workers (defaults to --system)")
	cmd.Flags().StringVar(&config.BackendName, "backend", BackendSGLang, "Inference backend")
	cmd.Flags().StringVar(&config.BackendVersion, "backend-version", "", "Backend version")
	cmd.Flags().IntVar(&config.Prefix, "prefix", 0, "Prefix cache length")
	cmd.Flags().StringVar(&config.DatabaseMode, "database-mode", DatabaseModeSilicon, "Database mode (SILICON, HYBRID, EMPIRICAL, SOL)")
	cmd.Flags().StringVar(&config.SaveDir, "save-dir", "", "Local directory to save generated YAML files (defaults to current directory)")

	// Job / image options
	cmd.Flags().StringVar(&storage, "storage", "", "Storage to use (overrides default from config)")
	cmd.Flags().StringVar(&image, "image", "", "Override the aiconfigurator container image")
	cmd.Flags().BoolVar(&skipDownload, "skip-download", false, "Skip downloading generated files to local disk (files remain in storage)")
	cmd.Flags().BoolVar(&silence, "silence", false, "Suppress pod log output during generation")

	// Mark required flags
	for _, flag := range []string{"model", "system", "total-gpus"} {
		if err := cmd.MarkFlagRequired(flag); err != nil {
			klog.Fatalf("Failed to mark flag %s as required: %v", flag, err)
		}
	}

	return cmd
}

// runGenerate is the main entry point for the generate command.
// It creates a Kubernetes Pod using the generator plugin, waits for completion,
// and optionally downloads the output to local disk.
// The Pod is always deleted on exit (normal, error, or interrupt via SIGINT/SIGTERM).
func runGenerate(cf *genericclioptions.ConfigFlags, taskCfg *TaskConfig, storage, image string, skipDownload, silence bool) error {
	fmt.Println("=== RBG LLM Generate ===")

	// Step 1: Validate configuration
	if err := validateConfig(taskCfg); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Step 2: Load CLI config and resolve storage
	cliCfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	storageName := cliCfg.CurrentStorage
	if storage != "" {
		storageName = storage
	}
	if storageName == "" {
		return fmt.Errorf("no storage configured, please run 'kubectl rbg llm config add-storage' first")
	}

	storageCfg, err := cliCfg.GetStorage(storageName)
	if err != nil {
		return err
	}

	storagePlugin, err := storageplugin.Get(storageCfg.Type, storageCfg.Config)
	if err != nil {
		return fmt.Errorf("failed to initialize storage plugin: %w", err)
	}
	if storagePlugin == nil {
		return fmt.Errorf("unknown storage type: %s", storageCfg.Type)
	}

	// Step 3: Resolve namespace and paths
	ns := util.GetNamespace(cf)
	mountPath := storagePlugin.MountPath()
	modelPath := filepath.Join(mountPath, taskCfg.ModelName)
	outputDir := OutputDir()

	// Step 4: Build plugin config
	pluginCfg := &plugin.Config{
		ModelName:        taskCfg.ModelName,
		SystemName:       taskCfg.SystemName,
		DecodeSystemName: taskCfg.DecodeSystemName,
		BackendName:      taskCfg.BackendName,
		BackendVersion:   taskCfg.BackendVersion,
		TotalGPUs:        taskCfg.TotalGPUs,
		ISL:              taskCfg.ISL,
		OSL:              taskCfg.OSL,
		TTFT:             taskCfg.TTFT,
		TPOT:             taskCfg.TPOT,
		RequestLatency:   taskCfg.RequestLatency,
		Prefix:           taskCfg.Prefix,
		DatabaseMode:     taskCfg.DatabaseMode,
		ModelPath:        modelPath,
		OutputDir:        outputDir,
		Namespace:        ns,
		Image:            image,
		ExtraArgs:        taskCfg.ExtraArgs,
	}

	// Step 5: Select generator plugin
	var generatorPlugin plugin.Plugin
	switch taskCfg.ConfiguratorTool {
	case AIConfigurator, "":
		generatorPlugin = &plugin.AIConfiguratorPlugin{}
	default:
		generatorPlugin = plugin.NewGeneralPlugin(taskCfg.ConfiguratorTool)
	}

	// Step 6: Create Pod executor and run the Pod
	executor, err := NewPodExecutor(cf, storagePlugin, storageName, ns)
	if err != nil {
		return err
	}

	// Set up a cancellable context driven by SIGINT/SIGTERM so that all
	// in-flight API calls are aborted promptly when the user interrupts.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nInterrupted, cleaning up...")
		cancel()
	}()

	fmt.Printf("Creating generate Pod for model %s...\n", taskCfg.ModelName)
	podName, err := executor.Run(ctx, pluginCfg, generatorPlugin)
	if err != nil {
		return err
	}

	// Always delete the Pod on exit (normal return, error, or signal-triggered cancel).
	// A background context is used so the deletion succeeds even when ctx is cancelled.
	defer func() {
		executor.Delete(context.Background(), podName)
	}()

	// Step 7: Wait for generate container to complete
	fmt.Printf("Waiting for Pod %s to complete...\n", podName)
	state, err := executor.Wait(ctx, podName, silence)
	if err != nil {
		return err
	}
	if state == PodStateFailed {
		return fmt.Errorf("generate pod %s failed, check logs with: kubectl logs %s -n %s -c %s", podName, podName, ns, ContainerNameGenerate)
	}
	fmt.Printf("\u2713 Generate Pod %s completed successfully\n", podName)

	// Step 8: Download output to local disk
	if skipDownload {
		fmt.Printf("Skipping download. Output files are in storage at: %s\n", outputDir)
		return nil
	}

	localDir := taskCfg.SaveDir
	if localDir == "" {
		localDir = "."
	}
	fmt.Printf("Downloading generated files to %s...\n", localDir)
	if err := executor.DownloadOutput(ctx, podName, outputDir, localDir); err != nil {
		return fmt.Errorf("failed to download output: %w", err)
	}

	fmt.Printf("\u2713 Generated YAML files saved to: %s\n", localDir)
	return nil
}

// validateConfig validates the TaskConfig
func validateConfig(config *TaskConfig) error {
	if strings.TrimSpace(config.ModelName) == "" {
		return fmt.Errorf("--model is required")
	}
	if strings.TrimSpace(config.SystemName) == "" {
		return fmt.Errorf("--system is required")
	}
	if config.TotalGPUs <= 0 {
		return fmt.Errorf("--total-gpus must be greater than 0")
	}

	// Validate latency parameters: at least one of (ttft & tpot) or request-latency must be set
	hasTTFTAndTPOT := config.TTFT > 0 && config.TPOT > 0
	hasRequestLatency := config.RequestLatency > 0
	if !hasTTFTAndTPOT && !hasRequestLatency {
		return fmt.Errorf("latency parameters validation failed: either (--ttft AND --tpot) or --request-latency must be specified with positive values")
	}

	// Validate enum values
	validBackends := map[string]bool{BackendSGLang: true, BackendVLLM: true, BackendTRTLLM: true}
	if !validBackends[config.BackendName] {
		return fmt.Errorf("invalid backend %s, must be one of: sglang, vllm, trtllm", config.BackendName)
	}

	validSystems := map[string]bool{
		"h100_sxm": true, "a100_sxm": true, "b200_sxm": true,
		"gb200_sxm": true, "l40s": true, "h200_sxm": true,
	}
	if !validSystems[config.SystemName] {
		return fmt.Errorf("invalid system %s, must be one of: h100_sxm, a100_sxm, b200_sxm, gb200_sxm, l40s, h200_sxm", config.SystemName)
	}

	validDatabaseModes := map[string]bool{
		DatabaseModeSilicon:   true,
		DatabaseModeHybrid:    true,
		DatabaseModeEmpirical: true,
		DatabaseModeSOL:       true,
	}
	if !validDatabaseModes[config.DatabaseMode] {
		return fmt.Errorf("invalid database-mode %s, must be one of: SILICON, HYBRID, EMPIRICAL, SOL", config.DatabaseMode)
	}

	return nil
}

func GetModelBaseName(modelName string) string {
	baseName := filepath.Base(path.Clean(modelName))
	if baseName == "." || baseName == string(filepath.Separator) || baseName == "" {
		return "" // Or some other indicator for "not found"
	}
	return baseName
}
