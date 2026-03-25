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
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	genericclioptions "k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/rbgs/cmd/cli/cmd/llm/generate/plugin"
	storageplugin "sigs.k8s.io/rbgs/cmd/cli/plugin/storage"
	"sigs.k8s.io/rbgs/cmd/cli/util"
)

const (
	// generatePodLabelKey is the label used to identify generate Pods.
	generatePodLabelKey = "rbg-generate-pod"
	// DefaultGenerateActiveDeadlineSeconds is the max duration for the generate Pod (30 minutes).
	DefaultGenerateActiveDeadlineSeconds int64 = 1800

	// outputVolumeName is the name of the emptyDir volume shared between containers.
	outputVolumeName = "generate-output"
	// ContainerNameGenerate is the name of the main generator container.
	ContainerNameGenerate = "generate"
	// containerNameWaiter is the name of the sidecar container that keeps the Pod alive for kubectl cp.
	containerNameWaiter = "waiter"
	// OutputDirInContainer is the path inside the container where generated files are written.
	// It is backed by an emptyDir volume so it is independent of the storage plugin type.
	OutputDirInContainer = "/tmp/output"

	// DefaultWaiterImage is the default image for the waiter sidecar container.
	// It only needs to run "sleep infinity" to keep the Pod alive for kubectl cp.
	// Override with --waiter-image in air-gapped environments where docker.io is unreachable.
	DefaultWaiterImage = "busybox:1.36"

	// DefaultDownloadTimeoutSeconds is the maximum time allowed for kubectl cp to transfer
	// generated output files from the waiter container to local disk (5 minutes).
	DefaultDownloadTimeoutSeconds = 5 * 60
)

// PodState represents the observed state of the generate Pod.
type PodState string

const (
	PodStatePending   PodState = "Pending"
	PodStateSucceeded PodState = "Succeeded"
	PodStateFailed    PodState = "Failed"
)

// sanitizeForPodName replaces characters not suitable for Kubernetes resource names with hyphens.
func sanitizeForPodName(s string) string {
	s = strings.ToLower(s)
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '.' {
			return r
		}
		return '-'
	}, s)
}

// PodExecutor handles creating, monitoring, and cleaning up a generate Pod.
// The Pod runs two containers:
//   - "generate": runs the generator tool and writes output to an emptyDir volume,
//     then exits normally.
//   - "waiter": sleeps indefinitely so the Pod stays Running after the generator
//     finishes, allowing kubectl cp to retrieve the output before cleanup.
//
// Lifecycle is fully managed by the caller via Run, Wait, DownloadOutput, and Delete.
type PodExecutor struct {
	cf            *genericclioptions.ConfigFlags
	clientset     *kubernetes.Clientset
	storagePlugin storageplugin.Plugin
	storageName   string
	namespace     string
}

// NewPodExecutor creates a PodExecutor.
func NewPodExecutor(cf *genericclioptions.ConfigFlags, storagePlugin storageplugin.Plugin, storageName, namespace string) (*PodExecutor, error) {
	clientset, err := util.GetK8SClientSet(cf)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes clientset: %w", err)
	}
	return &PodExecutor{
		cf:            cf,
		clientset:     clientset,
		storagePlugin: storagePlugin,
		storageName:   storageName,
		namespace:     namespace,
	}, nil
}

// Run creates a generate Pod from the given plugin config and returns the Pod name.
func (e *PodExecutor) Run(ctx context.Context, cfg *plugin.Config, p plugin.Plugin) (string, error) {
	// Build container from plugin
	container, err := p.Container(cfg)
	if err != nil {
		return "", fmt.Errorf("failed to build container spec: %w", err)
	}

	// Build pod spec
	podSpec := corev1.PodSpec{
		Containers:    []corev1.Container{container},
		RestartPolicy: corev1.RestartPolicyNever,
	}

	podTemplate := &corev1.PodTemplateSpec{Spec: podSpec}

	// Mount storage via storage plugin
	ctrlClient, err := util.GetControllerRuntimeClient(e.cf)
	if err != nil {
		return "", fmt.Errorf("failed to create controller client: %w", err)
	}
	if err := e.storagePlugin.MountStorage(podTemplate, storageplugin.MountOptions{
		Client:      ctrlClient,
		StorageName: e.storageName,
		Namespace:   e.namespace,
	}); err != nil {
		return "", fmt.Errorf("failed to mount storage: %w", err)
	}

	pod := buildGeneratePod(cfg.ModelName, podTemplate)
	created, err := e.clientset.CoreV1().Pods(e.namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to create generate pod: %w", err)
	}

	fmt.Printf("Created generate Pod %s in namespace %s\n", created.Name, e.namespace)
	return created.Name, nil
}

// Wait streams logs from the "generate" container and waits until it terminates.
// The "waiter" sidecar keeps the Pod Running so kubectl cp can retrieve output afterwards.
// Returns the final state based on the generate container's exit code.
// If silence is true, pod logs are not streamed to stdout.
func (e *PodExecutor) Wait(ctx context.Context, podName string, silence bool) (PodState, error) {
	if !silence {
		fmt.Printf("Streaming logs from pod %s (container %s)...\n", podName, ContainerNameGenerate)
		// Stream logs from the generate container; non-fatal if it fails
		if err := streamContainerLogs(ctx, e.clientset, e.namespace, podName, ContainerNameGenerate); err != nil {
			fmt.Printf("Warning: failed to stream logs: %v\n", err)
		}
	}

	// Poll until the generate container is Terminated
	exitCode, err := waitForContainerTerminated(ctx, e.clientset, e.namespace, podName, ContainerNameGenerate)
	if err != nil {
		return PodStatePending, err
	}
	if exitCode != 0 {
		return PodStateFailed, nil
	}
	return PodStateSucceeded, nil
}

// DownloadOutput copies files from the waiter sidecar container to a local directory.
// The waiter container is still Running at this point because only the generate container
// has exited; the waiter continues sleeping until the Pod is deleted.
//
// A dedicated timeout (DefaultDownloadTimeoutSeconds) is applied on top of the caller's
// context so that kubectl cp cannot hang indefinitely due to network stalls.
func (e *PodExecutor) DownloadOutput(ctx context.Context, podName, outputDir, localDir string) error {
	// Ensure local destination directory exists
	if err := os.MkdirAll(localDir, 0o755); err != nil {
		return fmt.Errorf("failed to create local directory %s: %w", localDir, err)
	}

	// Apply a dedicated download timeout so that kubectl cp cannot stall indefinitely
	// even when the caller's context has no deadline of its own.
	downloadCtx, cancel := context.WithTimeout(ctx, DefaultDownloadTimeoutSeconds*time.Second)
	defer cancel()

	// kubectl cp <namespace>/<pod>:<outputDir>/. <localDir> -c <waiter>
	src := fmt.Sprintf("%s/%s:%s/.", e.namespace, podName, outputDir)
	args := []string{"cp", src, localDir, "-c", containerNameWaiter}

	// Propagate kubeconfig and context so kubectl cp targets the same cluster
	// as the client-go calls in the rest of this executor.
	if e.cf != nil {
		if e.cf.KubeConfig != nil && *e.cf.KubeConfig != "" {
			args = append(args, "--kubeconfig", *e.cf.KubeConfig)
		}
		if e.cf.Context != nil && *e.cf.Context != "" {
			args = append(args, "--context", *e.cf.Context)
		}
	}

	cmd := exec.CommandContext(downloadCtx, "kubectl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Downloading output from %s (container: %s) to %s\n", src, containerNameWaiter, localDir)
	if err := cmd.Run(); err != nil {
		if downloadCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("kubectl cp timed out after %ds: %w", DefaultDownloadTimeoutSeconds, err)
		}
		return fmt.Errorf("kubectl cp failed: %w", err)
	}
	return nil
}

// Delete removes the generate Pod and its emptyDir volume.
// It is safe to call multiple times; a not-found error is silently ignored.
func (e *PodExecutor) Delete(ctx context.Context, podName string) {
	propagation := metav1.DeletePropagationForeground
	err := e.clientset.CoreV1().Pods(e.namespace).Delete(ctx, podName, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
	})
	if err != nil && !apierrors.IsNotFound(err) {
		fmt.Printf("Warning: failed to delete pod %s: %v\n", podName, err)
	} else if err == nil {
		fmt.Printf("Cleaned up generate Pod %s\n", podName)
	}
}

// OutputDir returns the container-side output directory for the generate Pod.
// Files are written to an emptyDir volume so the output is independent of the
// storage plugin type (PVC, OSS, etc.).
func OutputDir() string {
	return OutputDirInContainer
}

// buildGeneratePod constructs a Kubernetes Pod for the generate command.
// The Pod runs two containers:
//   - "generate": runs the generator tool, writes output to the shared emptyDir, then exits.
//   - "waiter": sleeps indefinitely so the Pod stays Running after the generator
//     finishes, giving kubectl cp time to retrieve the output.
//
// The Pod is not managed by a Job; its lifecycle is fully controlled by PodExecutor.
func buildGeneratePod(modelName string, podTemplate *corev1.PodTemplateSpec) *corev1.Pod {
	timestamp := time.Now().Unix()
	sanitizedModel := sanitizeForPodName(modelName)
	podName := fmt.Sprintf("generate-%s-%d", sanitizedModel, timestamp)

	labels := map[string]string{
		generatePodLabelKey: "true",
		"rbg-model-id":      sanitizedModel,
	}

	activeDeadlineSeconds := DefaultGenerateActiveDeadlineSeconds

	// Inject the shared emptyDir volume and its mount into every container in the template.
	podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, corev1.Volume{
		Name: outputVolumeName,
		VolumeSource: corev1.VolumeSource{
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
	})
	outputMount := corev1.VolumeMount{
		Name:      outputVolumeName,
		MountPath: OutputDirInContainer,
	}
	for i := range podTemplate.Spec.Containers {
		podTemplate.Spec.Containers[i].VolumeMounts = append(
			podTemplate.Spec.Containers[i].VolumeMounts,
			outputMount,
		)
	}

	// Add the waiter sidecar that keeps the Pod alive until kubectl cp completes.
	waiter := corev1.Container{
		Name:         containerNameWaiter,
		Image:        "busybox:1.36",
		Command:      []string{"/bin/sh", "-c", "sleep infinity"},
		VolumeMounts: []corev1.VolumeMount{outputMount},
	}
	podTemplate.Spec.Containers = append(podTemplate.Spec.Containers, waiter)

	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: podTemplate.Namespace,
			Labels:    labels,
		},
		Spec: corev1.PodSpec{
			Containers:            podTemplate.Spec.Containers,
			RestartPolicy:         corev1.RestartPolicyNever,
			Volumes:               podTemplate.Spec.Volumes,
			ActiveDeadlineSeconds: &activeDeadlineSeconds,
			ImagePullSecrets:      podTemplate.Spec.ImagePullSecrets,
			NodeSelector:          podTemplate.Spec.NodeSelector,
			Tolerations:           podTemplate.Spec.Tolerations,
			Affinity:              podTemplate.Spec.Affinity,
		},
	}
}

// streamContainerLogs waits for the specified container to start, then follows its
// logs until the container exits (EOF on the log stream).
func streamContainerLogs(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) error {
	if err := waitForContainerRunningOrDone(ctx, clientset, namespace, podName, containerName); err != nil {
		return err
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("failed to open log stream: %w", err)
	}
	defer func() { _ = stream.Close() }()

	reader := bufio.NewReader(stream)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			fmt.Print(line)
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error reading log stream: %w", err)
		}
	}
}

// waitForContainerRunningOrDone waits until the specified container is Running or Terminated.
func waitForContainerRunningOrDone(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) error {
	deadline := time.Now().Add(5 * time.Minute)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for container %s in pod %s to start", containerName, podName)
		}

		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get pod %s: %w", podName, err)
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != containerName {
				continue
			}
			if cs.State.Running != nil || cs.State.Terminated != nil {
				return nil
			}
			if cs.State.Waiting != nil && cs.State.Waiting.Reason != "" {
				fmt.Printf("Container %s is waiting: %s\n", containerName, cs.State.Waiting.Reason)
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

// waitForContainerTerminated polls until the specified container has terminated and returns its exit code.
func waitForContainerTerminated(ctx context.Context, clientset *kubernetes.Clientset, namespace, podName, containerName string) (int32, error) {
	deadline := time.Now().Add(time.Duration(DefaultGenerateActiveDeadlineSeconds) * time.Second)

	for {
		if time.Now().After(deadline) {
			return -1, fmt.Errorf("timeout waiting for container %s in pod %s to terminate", containerName, podName)
		}

		pod, err := clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return -1, fmt.Errorf("failed to get pod %s: %w", podName, err)
		}

		for _, cs := range pod.Status.ContainerStatuses {
			if cs.Name != containerName {
				continue
			}
			if cs.State.Terminated != nil {
				return cs.State.Terminated.ExitCode, nil
			}
		}

		select {
		case <-ctx.Done():
			return -1, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}
