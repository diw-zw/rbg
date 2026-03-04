# CLI Development Guide

This document describes how to test and develop the `kubectl rbg` command-line tool during the development phase.

## Quick Start

### Build

```bash
cd /Users/wangdi/github/diw-zw/rbg
go build -o kubectl-rbg ./cmd/cli/main.go
```

### Run with go run

```bash
cd /Users/wangdi/github/diw-zw/rbg
go run ./cmd/cli/main.go <command>
```

## Testing Without Kubernetes Cluster

The CLI tool can be tested in isolation without a running Kubernetes cluster for most commands.

### 1. Config Commands

Config commands operate on a local YAML file and do not require cluster access:

```bash
# Use a temporary config file for testing
export RBG_CONFIG=/tmp/rbg-test-config.yaml

# Test config commands
go run ./cmd/cli/main.go llm config init
go run ./cmd/cli/main.go llm config view
go run ./cmd/cli/main.go llm config add-storage my-pvc --type pvc --config pvcName=models-pvc
go run ./cmd/cli/main.go llm config add-source huggingface --type huggingface
go run ./cmd/cli/main.go llm config add-engine vllm --type vllm
go run ./cmd/cli/main.go llm config get-storages
go run ./cmd/cli/main.go llm config get-sources
go run ./cmd/cli/main.go llm config get-engines
```

### 2. Pull Command (Template Generation Only)

The `pull` command generates a Pod template YAML without actually creating resources:

```bash
export RBG_CONFIG=/tmp/rbg-test-config.yaml

# Initialize config first
go run ./cmd/cli/main.go llm config init

# Generate download template (prints YAML to stdout)
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf

# With specific source/storage
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf --source huggingface --storage my-pvc
```

### 3. Run Command (Template Generation Only)

The `run` command generates a Pod template for model serving:

```bash
export RBG_CONFIG=/tmp/rbg-test-config.yaml

# Generate serving template
go run ./cmd/cli/main.go llm run meta-llama/Llama-2-7b-hf

# With resource overrides
go run ./cmd/cli/main.go llm run meta-llama/Llama-2-7b-hf --gpu 2 --memory 32Gi
```

## Testing with Kubernetes Cluster

### Prerequisites

1. A running Kubernetes cluster (kind, minikube, or remote cluster)
2. kubectl configured to access the cluster
3. RBG controller installed (for actual resource creation)

### Set Up Test Environment

```bash
# Create a test namespace
kubectl create namespace rbg-test

# Set as default namespace for CLI
export RBG_CONFIG=/tmp/rbg-test-config.yaml
go run ./cmd/cli/main.go llm config set-namespace rbg-test
```

### Test Resource Creation

After generating templates, you can apply them manually:

```bash
# Generate and apply pull template
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf > /tmp/download-job.yaml
kubectl apply -f /tmp/download-job.yaml

# Generate and apply run template
go run ./cmd/cli/main.go llm run meta-llama/Llama-2-7b-hf > /tmp/serve-pod.yaml
kubectl apply -f /tmp/serve-pod.yaml
```

## Plugin Development Testing

### Test Storage Plugin (PVC)

```bash
# Add PVC storage with validation
go run ./cmd/cli/main.go llm config add-storage test-pvc --type pvc --config pvcName=test-pvc

# This validates that pvcName is provided (required field)
# Test validation failure (should error):
go run ./cmd/cli/main.go llm config add-storage test-pvc --type pvc --config invalidKey=value
```

### Test Source Plugin (HuggingFace)

```bash
# Add HuggingFace source with mirror
go run ./cmd/cli/main.go llm config add-source hf --type huggingface --config mirror=https://hf-mirror.com

# Generate pull template and verify HF_ENDPOINT is set
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf --source hf
```

### Test Engine Plugin (vLLM/SGLang)

```bash
# Add vLLM engine with custom image
go run ./cmd/cli/main.go llm config add-engine vllm-custom --type vllm --config image=myregistry/vllm:v1.0

# Generate run template
go run ./cmd/cli/main.go llm run meta-llama/Llama-2-7b-hf --engine vllm-custom
```

## Debugging Tips

### Verbose Output

Add print statements to plugin code and rebuild:

```bash
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf 2>&1 | head -50
```

### Check Generated Templates

Always verify the generated YAML before applying:

```bash
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf | cat
```

### Validate Config Changes

```bash
# View current config
cat $RBG_CONFIG

# Or use the view command
go run ./cmd/cli/main.go llm config view
```

## Continuous Testing During Development

### Watch Mode (using air or similar)

Install `air` for auto-reload during development:

```bash
go install github.com/cosmtrek/air@latest

# Create .air.toml in cmd/cli directory
cd cmd/cli
cat > .air.toml << 'EOF'
root = "."
tmp_dir = "tmp"

[build]
cmd = "go build -o ./tmp/main ./main.go"
bin = "tmp/main"
full_bin = "./tmp/main llm config view"
EOF

air
```

### Unit Tests

Run unit tests for specific packages:

```bash
# Test config package
go test ./cmd/cli/config/...

# Test plugin packages
go test ./cmd/cli/plugin/...
```

## Common Issues

### Config File Not Found

Ensure `RBG_CONFIG` is set or the default path `~/.rbg/config` exists:

```bash
mkdir -p ~/.rbg
touch ~/.rbg/config
```

### Plugin Not Registered

If you see "unknown storage type" errors, ensure plugin imports are present in `cmd/cli/cmd/llm/llm.go`:

```go
import (
    _ "sigs.k8s.io/rbgs/cmd/cli/plugin/engine"
    _ "sigs.k8s.io/rbgs/cmd/cli/plugin/source"
    _ "sigs.k8s.io/rbgs/cmd/cli/plugin/storage"
)
```

### Validation Errors

When adding storage/source/engine, validation errors indicate:
- Missing required fields
- Unknown fields (not declared in plugin's ConfigFields)
- Invalid plugin type

Example error:
```
Error: required config field "pvcName" is missing (hint: name of the pre-existing PersistentVolumeClaim to bind to)
```

## Integration Test Script

A complete test flow:

```bash
#!/bin/bash
set -e

export RBG_CONFIG=/tmp/rbg-test-config.yaml
rm -f $RBG_CONFIG

echo "=== Testing config commands ==="
go run ./cmd/cli/main.go llm config init
go run ./cmd/cli/main.go llm config add-storage pvc1 --type pvc --config pvcName=models-pvc
go run ./cmd/cli/main.go llm config add-source hf --type huggingface --config mirror=https://hf-mirror.com
go run ./cmd/cli/main.go llm config add-engine vllm --type vllm
go run ./cmd/cli/main.go llm config view

echo "=== Testing pull template generation ==="
go run ./cmd/cli/main.go llm pull meta-llama/Llama-2-7b-hf | head -30

echo "=== Testing run template generation ==="
go run ./cmd/cli/main.go llm run meta-llama/Llama-2-7b-hf --gpu 1 | head -30

echo "=== All tests passed ==="
```

Save as `test-cli.sh` and run:

```bash
chmod +x test-cli.sh
./test-cli.sh
```
