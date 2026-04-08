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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/rbgs/cmd/cli/plugin/util"
)

func init() {
	Register("sglang", func() Plugin {
		return &SGLangEngine{}
	})
}

// SGLangEngine implements the EnginePlugin interface for SGLang
type SGLangEngine struct {
	Image string
	Port  int32
}

// Name returns the plugin name
func (s *SGLangEngine) Name() string {
	return "sglang"
}

// ConfigFields returns the config fields this plugin accepts
func (s *SGLangEngine) ConfigFields() []util.ConfigField {
	return []util.ConfigField{
		{Key: "image", Description: "SGLang container image (default: lmsysorg/sglang:latest)", Required: false},
		{Key: "port", Description: "port the server listens on (default: 30000)", Required: false},
	}
}

// Init initializes the plugin with config
func (s *SGLangEngine) Init(config map[string]interface{}) error {
	if image, ok := config["image"].(string); ok {
		s.Image = image
	} else {
		s.Image = "lmsysorg/sglang:latest"
	}
	if port, ok := config["port"].(int); ok {
		s.Port = int32(port)
	} else {
		s.Port = 30000
	}
	return nil
}

// GenerateTemplate generates a pod template for running SGLang
func (s *SGLangEngine) GenerateTemplate(opts GenerateOptions) (*corev1.PodTemplateSpec, error) {
	// Use override image if provided, otherwise use default
	image := s.Image
	if opts.Image != "" {
		image = opts.Image
	}

	// Build base args
	args := []string{
		"--model-path",
		opts.ModelPath,
		"--served-model-name",
		opts.Name,
	}

	// Add distributed deployment args for multi-node setup
	if opts.DistributedSize > 1 {
		args = append(args,
			"--dist-init-addr=$(RBG_LWP_LEADER_ADDRESS):6379",
			"--nnodes=$(RBG_LWP_GROUP_SIZE)",
			"--node-rank=$(RBG_LWP_WORKER_INDEX)",
		)
	}

	// Add user-provided args
	args = append(args, opts.Args...)

	// Build env vars
	env := []corev1.EnvVar{
		{
			Name:  "SGLANG_MODEL_PATH",
			Value: opts.ModelPath,
		},
	}
	env = append(env, opts.Env...)

	return &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "sglang",
					Image:           image,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Command:         []string{"python", "-m", "sglang.launch_server"},
					Args:            args,
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							ContainerPort: s.Port,
						},
					},
					Env:       env,
					Resources: opts.Resources,
				},
			},
		},
	}, nil
}
