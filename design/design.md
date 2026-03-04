# 接口设计
## 插件接口
### Storage 插件
以插件的形式支持不同的存储方式，比如 oss、s3、pvc 等。
Storage 插件的主要作用是：
1. 将存储服务以 PVC 的形式绑定在工作负载上，工作负载包括数据下载任务、模型服务。

```go
type StoragePlugin interface {
    Name() string

    // Initialize storage with config
    Init(config map[string]interface{}) error

    // Check if model exists
    Exists(modelID string) (bool, error)

    // Progress to exec before mount, like creating pvc/pv.
    PreMount(client client.Client, modelID string, revision string) error
    
    // MountStorage configures the pod template to mount model storage
    // The plugin directly modifies the PodTemplateSpec to add necessary:
    // - Volumes
    // - VolumeMounts
    // - InitContainers (if needed)
    // Returns the mount path where the model will be accessible in the container
    MountStorage(modelID string, revision string, podTemplate *corev1.PodTemplateSpec) error

    // Get Storage MountPath.
    MountPath(modelID string) (string)
}

type StorageFactory func() StoragePlugin
func RegisterStorage(name string, factory StorageFactory) error { ... }
func GetSrorage(name string, config map[string]interface{}) StoragePlugin { ... }
```

#### Example
```go
type OSSStorage struct {
    EndPoint  string
    Bucket    string
    AccessKey string
    SecretKey string
}

func (s OSSStorage) Name() string {return "oss"}
func (s OSSStorage) Init(config map[string]interface{}) error {...}
func (s OSSStorage) Exists(modelID string) (bool, error) {...}
func (s OSSStorage) PreMount(client client.Client, modelID string, revision string) error
func (s OSSStorage) MountStorage(modelID string, revision string, podTemplate *corev1.PodTemplateSpec) error {...}
func (s OSSStorage) MountPath(modelID string, revision string) string {return "/oss/{path}"}

func init() {
    RegisterStorage("oss", func() StoragePlugin {
        return &OSSStorage{}
    })
}
```

### Source 插件
Source 插件的主要作用是：
1. 生成数据下载任务的工作负载。

```go
type SourcePlugin interface {
    Name() string

    // Initialize with credentials/config
    Init(config map[string]interface{}) error

    // Generate podTemplate used to download model.
    GenerateTemplate(modelID string, modelPath string) (*corev1.PodTemplateSpec, error)
}
```

### Engine 插件
engine 插件的主要作用是：
1. 生成模型服务的工作负载。
2. 获取模型服务的访问地址。

```go
type EnginePlugin interface {
    Name() string

    // Initialize with credentials/config
    Init(config map[string]interface{}) error

    // Generate podTemplate used to start model engine.
    GenerateTemplate(name string, modelID string, modelPath string) (*corev1.PodTemplateSpec, error)

    // Get model service url.
    GetModelService(name string) (string, error)
}
```

## config 文件内容
记录用户已配置的以及当前正使用的数据源、存储和推理引擎。
默认存储在 ～/.rbg/config，可通过 RBG_CLI_CONFIG 环境变量覆盖。

```yaml
apiVersion: rbg/v1alpha1
kind: Config

storages:
  - name: oss-beijing
    type: oss
    config:
      endpoint: oss-cn-beijing.aliyuncs.com
      bucket: llm-models
      accessKey: "<ACCESS_KEY>"
      secretKey: "<SECRET_KEY>"

sources:
  - name: huggingface
    type: huggingface
    config:
      token: "hf_xxx"
      mirror: "https://hf-mirror.com"  # optional mirror for China

engines:
  - name: vllm
    type: vllm
    config:
      image: vllm/vllm-openai:v0.3.0
      port: 8000

current-storage: oss-beijing
current-source: huggingface
current-engine: vllm
namespace: llm-prod
```

# 执行逻辑
## 下载模型

1. 注册插件（自定义插件需从 ～/.rbg/plugin 文件夹中读取并注册）。
2. 读取配置文件，配置文件默认在 ~/.rbg/config 路径下，可通过 RBG_CONFIG 修改。
3. 使用上下文配置初始化插件。
4. 生成下载任务模版：
  a. 使用 Source 插件生成下载任务模版。
  b. 使用 Storage 插件为任务模版挂载存储位置。
5. 在集群中创建工作负载。

```go
// 伪代码
// 1. 通过 init() 注册插件

// 2. read config from flag or RBG_CONFIG or .rbg/config
option := yaml.Decode()

// 3. init plugin
storage := GetStorage(option.currentStroage, currentStorageConfig(option))
source := GetSource(option.currentSource, currentSourceConfig(option))

// 4. create job template

// 4.1 get storage mountPath
mountPath := storage.GetMountPath(args.ModelID)
// 4.2 generate download podTemplate
specTemplate := source.GenerateTemplate(args.ModelID, mountPath)
// 4.3 mount storage volume to podTemplate
storage.MountStorage(args.ModelID, specTemplate)

// 5. generate and create download job
client.Create(generateJob(specTemplate), option)
```

## 部署模型

1. 注册插件（自定义插件需从 ～/.rbg/plugin 文件夹中读取并注册）。
2. 读取配置文件，配置文件默认在 ~/.rbg/config 路径下，可通过 RBG_CONFIG 修改。
3. 使用上下文配置初始化插件。
4. 生成模型服务部署模版：
  a. 使用 Engine 插件生成部署模版。
  b. 使用 Storage 插件为部署模版挂载存储位置。
5. 在集群中创建模型服务。

```go
// 伪代码
// 1. 通过 init() 注册插件

// 2. read config from flag or RBG_CONFIG or .rbg/config
option := yaml.Decode(...)

// 3. init plugin
storage := GetStorage(option.currentStroage, currentStorageConfig(option))
engine := GetEngine(option.currentEngine)

// 4. create serving RBG

// 4.1 get storage mountPath
mountPath := storage.GetMountPath(args.ModelID)
// 4.2 generate download podTemplate
specTemplate := engine.GenerateTemplate(args.Name, args.ModelID, mountPath)
// 4.3 mount storage volume to podTemplate
storage.MountStorage(args.ModelID, specTemplate)

// 5. generate and create rbg
client.Create(generateRBG(specTemplate), option)
```
