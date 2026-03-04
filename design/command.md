# ============================================================
# Step 1: 添加、修改、查看、删除上下文
# Context Management (类似 kubectl config)
# ============================================================

# 添加存储配置（支持多个）
kubectl rbg llm config add-storage oss-beijing \
  --type oss \
  --endpoint oss-cn-beijing.aliyuncs.com \
  --bucket llm-models \
  --access-key ${ACCESS_KEY} \
  --secret-key ${SECRET_KEY}

# 添加模型源配置（支持多个）
kubectl rbg llm config add-source huggingface \
  --type huggingface \
  --token hf_xxx \
  --mirror https://hf-mirror.com  # 可选，国内镜

# 添加引擎配置（支持多个）
kubectl rbg llm config add-engine vllm \
  --image vllm/vllm-openai:v0.3.0 \
  --port 8000
  
# ============================================================
# 查看配置
# ============================================================

# 查看所有 storage 配置
kubectl rbg llm config get-storages
# Output:
# NAME            TYPE       CURRENT
# oss-beijing     oss        *

# 查看所有 source 配置
kubectl rbg llm config get-sources
# Output:
# NAME            TYPE            CURRENT
# huggingface     huggingface     *


# 查看所有 engine 配置
kubectl rbg llm config get-engine
# Output:
# NAME            TYPE            CURRENT
# vllm            vllm            *

# 查看当前配置
kubectl rbg llm config view
# Output:
# Current Configuration:
#
# Storage: oss-beijing (active)
#   Type: oss
#   Config: 
#     endpoint: oss-cn-beijing.aliyuncs.com
#     bucket: llm-models
#
# Source: huggingface (active)
#   Type: huggingface
#   Config: 
#     mirror: https://hf-mirror.com
#
# Engine: vllm
# Default Namespace: default

# ============================================================
# 切换配置
# ============================================================

# 切换默认 storage
kubectl rbg llm config use-storage oss-hangzhou

# 切换默认 source
kubectl rbg llm config use-source modelscope

# 切换默认 engine
kubectl rbg llm config use-engine vllm

# 切换默认 namespace
kubectl rbg llm config set namespace llm-production

# ============================================================
# 修改配置
# ============================================================

# 修改 storage 配置
kubectl rbg llm config set-storage oss-beijing \
  --endpoint oss-cn-beijing-internal.aliyuncs.com

# 修改 source 配置
kubectl rbg llm config set-source huggingface \
  --mirror https://new-mirror.com

# 修改 engine 配置
kubectl rbg llm config set-source vllm \
  --port 8001

# ============================================================
# 删除配置
# ============================================================

# 删除 storage
kubectl rbg llm config delete-storage oss-hangzhou

# 删除 source
kubectl rbg llm config delete-source huggingface

# 删除 source
kubectl rbg llm config delete-engine vllm

# 注意：删除 storage、 source 或 engine 前会检查是否为当前默认配置

# ============================================================
# 交互式初始化（向导模式，首次使用推荐）
# ============================================================

kubectl rbg llm config init --interactive
# 会引导用户:
# 1. 选择并配置 storage
# 2. 选择并配置 source
# 3. 选择 engine
# 4. 设置默认 namespace
# 5. 保存配置

# ============================================================
# Step 2: 拉取模型（使用当前默认配置或临时指定）
# ============================================================

# 使用当前默认配置
kubectl rbg llm pull Qwen/Qwen3.5-397B-A17B --revision main

# # 临时指定 source（不改变默认配置）
# kubectl rbg llm pull Qwen/Qwen3.5-397B-A17B \
#   --source modelscope \
#   --revision main
# 
# # 临时指定 storage（不改变默认配置）
# kubectl rbg llm pull Qwen/Qwen3.5-397B-A17B \
#   --storage local-nfs
# 
# # 同时指定 source 和 storage
# kubectl rbg llm pull Qwen/Qwen3.5-397B-A17B \
#   --source huggingface \
#   --storage oss-beijing

# ============================================================
# Step 3: 查看已下载的模型
# ============================================================

# 查看当前 storage 中的所有模型
kubectl rbg llm models
# Output:
# STORAGE         MODEL                           SIZE      STATUS      DOWNLOADED
# oss-beijing     Qwen/Qwen3.5-397B-A17B         740 GB    Complete    2024-03-02
# oss-beijing     Qwen/Qwen3.5-72B-Instruct      145 GB    Complete    2024-03-01

# ============================================================
# Step 4: 部署模型（使用当前默认配置）
# ============================================================

# 使用当前默认配置部署
kubectl rbg llm run Qwen/Qwen3.5-397B-A17B \
  --name qwen35 \
  --gpu 8

# 临时指定 storage 和 engine
kubectl rbg llm run Qwen/Qwen3.5-72B-Instruct \
  --name qwen72b \
  --storage oss-hangzhou \
  --engine sglang \
  --gpu 4

# 完整的部署命令示例
kubectl rbg llm run Qwen/Qwen3.5-397B-A17B \
  --name qwen35-prod \
  --replicas 2 \
  --gpu 8 \
  --cpu 32 \
  --memory 256Gi \
  --env VLLM_ATTENTION_BACKEND=FLASH_ATTN \
  --arg "--max-model-len=32768" \
  --arg "--tensor-parallel-size=8"

# ============================================================
# Step 5: 管理已部署的模型
# ============================================================

# 查看所有已部署的模型
kubectl rbg llm list
# Output:
# NAME            MODEL                           ENGINE    REPLICAS    STATUS      ENDPOINT
# qwen35          Qwen/Qwen3.5-397B-A17B         vllm      1/1         Running     qwen35.llm-prod:8000
# qwen72b         Qwen/Qwen3.5-72B-Instruct      sglang    2/2         Running     qwen72b.llm-prod:30000

# 查看特定 namespace 的部署
kubectl rbg llm list --namespace llm-dev

# 查看所有 namespace 的部署
kubectl rbg llm list --all-namespaces

# 查看部署详情
kubectl rbg llm status qwen35

# ============================================================
# Step 6: 访问模型服务
# ============================================================

# 交互式聊天
kubectl rbg llm chat --name qwen35

# 通过 API 测试
kubectl rbg llm test --name qwen35 --prompt "Hello, how are you?"

# 暴露服务
kubectl rbg llm expose qwen35

# ============================================================
# 完整工作流示例
# ============================================================

# 场景 1: 生产环境使用 OSS + HuggingFace
kubectl rbg llm config use-storage oss-beijing
kubectl rbg llm config use-source huggingface
kubectl rbg llm config use-engine vllm
kubectl rbg llm pull Qwen/Qwen3.5-397B-A17B
kubectl rbg llm run Qwen/Qwen3.5-397B-A17B --name qwen-prod --gpu 8
kubectl rbg llm chat --name qwen-prod

# 场景 2: 切换到开发环境使用 NFS + ModelScope
kubectl rbg llm config use-storage local-nfs
kubectl rbg llm config use-source modelscope
kubectl rbg llm config set engine sglang
kubectl rbg llm pull Qwen/Qwen3.5-7B
kubectl rbg llm run Qwen/Qwen3.5-7B --name qwen-dev --gpu 1

# 场景 3: 快速测试，临时指定配置（不改变默认配置）
kubectl rbg llm pull meta-llama/Llama-2-7b-hf \
  --source huggingface \
  --storage local-pvc
kubectl rbg llm run meta-llama/Llama-2-7b-hf \
  --name llama-test \
  --storage local-pvc \
  --engine vllm \
  --gpu 1
