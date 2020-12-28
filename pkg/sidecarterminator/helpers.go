package sidecarterminator

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func podName(pod *v1.Pod) string {
	return fmt.Sprintf("%s/%s", pod.Namespace, pod.Name)
}

func isOwnedByJob(references []metav1.OwnerReference) bool {
	for _, ref := range references {
		// Jobs
		if ref.APIVersion == "batch/v1" && ref.Kind == "Job" {
			return true
		}

		// Argo Workflows
		if strings.HasPrefix(ref.APIVersion, "argoproj.io/") && ref.Kind == "Workflow" {
			return true
		}
	}

	return false
}

func isSidecarContainer(name string, sidecars map[string]int) bool {
	for containerName := range sidecars {
		if name == containerName {
			return true
		}
	}

	return false
}

func isCompleted(pod *v1.Pod, sidecars map[string]int) bool {
	if pod.Status.Phase == v1.PodRunning {
		complete := true

		for _, containerStatus := range pod.Status.ContainerStatuses {
			if isSidecarContainer(containerStatus.Name, sidecars) {
				continue
			}

			complete = complete && containerStatus.State.Terminated != nil && containerStatus.State.Terminated.ExitCode == 0
		}

		return complete
	}

	return false
}
