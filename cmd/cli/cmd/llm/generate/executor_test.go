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
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ---- sanitizeForPodName ----

func TestSanitizeForPodName_Slash(t *testing.T) {
	assert.Equal(t, "qwen-qwen3-32b", sanitizeForPodName("Qwen/Qwen3-32B"))
}

func TestSanitizeForPodName_Underscores(t *testing.T) {
	assert.Equal(t, "my-model", sanitizeForPodName("my_model"))
}

func TestSanitizeForPodName_AlreadyClean(t *testing.T) {
	assert.Equal(t, "my-model-7b", sanitizeForPodName("my-model-7b"))
}

func TestSanitizeForPodName_AllLower(t *testing.T) {
	result := sanitizeForPodName("MyModel")
	assert.Equal(t, strings.ToLower(result), result)
}

func TestSanitizeForPodName_SpecialChars(t *testing.T) {
	// @ and # should become hyphens
	result := sanitizeForPodName("model@v1#test")
	assert.Equal(t, "model-v1-test", result)
}

// ---- OutputDir ----

func TestOutputDir(t *testing.T) {
	result := OutputDir()
	assert.Equal(t, OutputDirInContainer, result)
}

// ---- buildGeneratePod ----

func TestBuildGeneratePod_Name(t *testing.T) {
	podTemplate := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: ContainerNameGenerate, Image: "test-image"}},
		},
	}
	pod := buildGeneratePod("Qwen/Qwen3-32B", podTemplate)

	assert.NotEmpty(t, pod.Name)
	assert.True(t, strings.HasPrefix(pod.Name, "generate-qwen-qwen3-32b-"), "pod name should start with sanitized model name")
}

func TestBuildGeneratePod_Labels(t *testing.T) {
	podTemplate := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{{Name: ContainerNameGenerate, Image: "test-image"}},
		},
	}
	pod := buildGeneratePod("mymodel", podTemplate)

	assert.Equal(t, "true", pod.Labels[generatePodLabelKey])
	assert.Equal(t, "mymodel", pod.Labels["rbg-model-id"])
}

func TestBuildGeneratePod_Spec(t *testing.T) {
	container := corev1.Container{Name: ContainerNameGenerate, Image: "test-image"}
	podTemplate := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{container},
		},
	}
	pod := buildGeneratePod("mymodel", podTemplate)

	// Pod gets the generate container + waiter sidecar
	assert.Equal(t, 2, len(pod.Spec.Containers))
	assert.Equal(t, ContainerNameGenerate, pod.Spec.Containers[0].Name)
	assert.Equal(t, "test-image", pod.Spec.Containers[0].Image)
	// Waiter sidecar should be the second container
	assert.Equal(t, containerNameWaiter, pod.Spec.Containers[1].Name)

	// emptyDir volume should be added
	assert.Equal(t, 1, len(pod.Spec.Volumes))
	assert.Equal(t, outputVolumeName, pod.Spec.Volumes[0].Name)
	assert.NotNil(t, pod.Spec.Volumes[0].VolumeSource.EmptyDir)

	// Both containers should have the output volume mounted
	assert.Equal(t, 1, len(pod.Spec.Containers[0].VolumeMounts))
	assert.Equal(t, outputVolumeName, pod.Spec.Containers[0].VolumeMounts[0].Name)
	assert.Equal(t, OutputDirInContainer, pod.Spec.Containers[0].VolumeMounts[0].MountPath)
	assert.Equal(t, 1, len(pod.Spec.Containers[1].VolumeMounts))
	assert.Equal(t, outputVolumeName, pod.Spec.Containers[1].VolumeMounts[0].Name)

	// RestartPolicy must be Never
	assert.Equal(t, corev1.RestartPolicyNever, pod.Spec.RestartPolicy)

	// ActiveDeadlineSeconds should be set
	assert.NotNil(t, pod.Spec.ActiveDeadlineSeconds)
	assert.Equal(t, DefaultGenerateActiveDeadlineSeconds, *pod.Spec.ActiveDeadlineSeconds)
}

func TestBuildGeneratePod_ObjectMeta(t *testing.T) {
	podTemplate := &corev1.PodTemplateSpec{}
	pod := buildGeneratePod("mymodel", podTemplate)
	// Pod should have ObjectMeta populated
	assert.NotEmpty(t, pod.Name)
	assert.Equal(t, metav1.ObjectMeta{
		Name:      pod.Name,
		Namespace: pod.Namespace,
		Labels:    pod.Labels,
	}, pod.ObjectMeta)
}
