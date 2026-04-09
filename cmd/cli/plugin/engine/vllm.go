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

package engine

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/rbgs/cmd/cli/plugin/util"
)

func init() {
	Register("vllm", func() Plugin {
		return &VLLMEngine{}
	})
}

// VLLMEngine implements the EnginePlugin interface for vLLM
type VLLMEngine struct {
	Image string
	Port  int32
}

// Name returns the plugin name
func (v *VLLMEngine) Name() string {
	return "vllm"
}

// ConfigFields returns the config fields this plugin accepts
func (v *VLLMEngine) ConfigFields() []util.ConfigField {
	return []util.ConfigField{
		{Key: "image", Description: "vLLM container image (default: vllm/vllm-openai:latest)", Required: false},
		{Key: "port", Description: "port the server listens on (default: 8000)", Required: false},
	}
}

// Init initializes the plugin with config
func (v *VLLMEngine) Init(config map[string]interface{}) error {
	if image, ok := config["image"].(string); ok {
		v.Image = image
	} else {
		v.Image = "vllm/vllm-openai:latest"
	}
	if port, ok := config["port"].(int); ok {
		v.Port = int32(port)
	} else {
		v.Port = 8000
	}
	return nil
}

// GenerateTemplate generates a pod template for running vLLM
func (v *VLLMEngine) GenerateTemplate(opts GenerateOptions) (*corev1.PodTemplateSpec, error) {
	// Use override image if provided, otherwise use default
	image := v.Image
	if opts.Image != "" {
		image = opts.Image
	}

	// Build base args
	args := []string{
		"--model",
		opts.ModelPath,
		"--served-model-name",
		opts.Name,
	}

	// Add user-provided args
	args = append(args, opts.Args...)

	podSpec := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "vllm",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Args:            args,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: v.Port,
						},
					},
					Env:       opts.Env,
					Resources: opts.Resources,
				},
			},
		},
	}

	// Add shared memory volume if ShmSize is specified
	if opts.ShmSize != "" {
		shmQuantity, err := resource.ParseQuantity(opts.ShmSize)
		if err != nil {
			return nil, fmt.Errorf("invalid shmSize %q: %w", opts.ShmSize, err)
		}

		// Add volume to pod
		podSpec.Spec.Volumes = append(podSpec.Spec.Volumes, corev1.Volume{
			Name: "shm",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{
					Medium:    corev1.StorageMediumMemory,
					SizeLimit: &shmQuantity,
				},
			},
		})

		// Add volume mount to container
		podSpec.Spec.Containers[0].VolumeMounts = append(podSpec.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "shm",
			MountPath: "/dev/shm",
		})
	}

	return podSpec, nil
}
