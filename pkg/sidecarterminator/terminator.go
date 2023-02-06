package sidecarterminator

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
)

const (
	SIDECAR_TERMINATOR_CONTAINER = "sidecar-terminator"
)

// SidecarTerminator defines an instance of the sidecar terminator.
type SidecarTerminator struct {
	config    *rest.Config
	clientset *kubernetes.Clientset

	eventHandler *sidecarTerminatorEventHandler

	sidecars   map[string]int
	namespaces []string
}

// NewSidecarTerminator returns a new SidecarTerminator instance.
func NewSidecarTerminator(config *rest.Config, clientset *kubernetes.Clientset, sidecarsstr, namespaces []string) (*SidecarTerminator, error) {
	if config == nil {
		return nil, errors.New("config cannot be nil")
	}

	if clientset == nil {
		return nil, errors.New("clientset cannot be nil")
	}

	sidecars := map[string]int{}
	for _, sidecar := range sidecarsstr {
		comp := strings.Split(sidecar, "=")
		if len(comp) == 1 {
			sidecars[comp[0]] = int(syscall.SIGTERM)
		} else if len(comp) == 2 {
			num, err := strconv.Atoi(comp[1])
			if err != nil {
				return nil, err
			}

			sidecars[comp[0]] = num
		} else {
			return nil, fmt.Errorf("incorrect sidecar container name format: %s", sidecar)
		}
	}

	return &SidecarTerminator{
		config:     config,
		clientset:  clientset,
		sidecars:   sidecars,
		namespaces: namespaces,
	}, nil
}

func (st *SidecarTerminator) setupInformerForNamespace(ctx context.Context, namespace string) error {
	if namespace == v1.NamespaceAll {
		klog.Info("starting shared informer")
	} else {
		klog.Infof("starting shared informer for namespace %q", namespace)
	}

	factory := informers.NewFilteredSharedInformerFactory(
		st.clientset,
		time.Minute*10,
		namespace,
		nil,
	)

	factory.Core().V1().Pods().Informer().AddEventHandler(st.eventHandler)
	factory.Start(ctx.Done())
	for _, ok := range factory.WaitForCacheSync(nil) {
		if !ok {
			return errors.New("timed out waiting for controller caches to sync")
		}
	}

	return nil
}

// Run runs the sidecar terminator.
// TODO: Ensure this is only called once..
func (st *SidecarTerminator) Run(ctx context.Context) error {
	klog.Info("starting sidecar terminator")

	// Setup event handler
	st.eventHandler = &sidecarTerminatorEventHandler{
		st: st,
	}

	// Setup shared informer factory
	if len(st.namespaces) == 0 {
		if err := st.setupInformerForNamespace(ctx, metav1.NamespaceAll); err != nil {
			return err
		}
	} else {
		for _, namespace := range st.namespaces {
			if err := st.setupInformerForNamespace(ctx, namespace); err != nil {
				return err
			}
		}
	}

	<-ctx.Done()
	klog.Info("terminating sidecar terminator")
	return nil
}

func (st *SidecarTerminator) terminate(pod *v1.Pod) error {
	klog.Infof("Found running sidecar containers in %s", podName(pod))

	// Terminate the sidecar
	for _, sidecar := range pod.Status.ContainerStatuses {
		if isSidecarContainer(sidecar.Name, st.sidecars) && sidecar.State.Running != nil && !hasSidecarTerminator(pod, sidecar) {

			// TODO: Add ability to kill the proper process
			// May require looking into the OCI image to extract the entrypoint if not
			// available via the containers' command.
			if *pod.Spec.ShareProcessNamespace {
				klog.Error("Containers are sharing process namespace: ending process 1 will not end sidecars.")
				return fmt.Errorf("unable to end sidecar %s in pod %s using shareProcessNamespace", sidecar.Name, podName(pod))
			}

			klog.Infof("Terminating sidecar %s from %s with signal %d", sidecar.Name, podName(pod), st.sidecars[sidecar.Name])

			pod.Spec.EphemeralContainers = append(pod.Spec.EphemeralContainers, v1.EphemeralContainer{
				TargetContainerName: sidecar.Name,
				EphemeralContainerCommon: v1.EphemeralContainerCommon{
					Name:  generateSidecarTerminatorName(sidecar.Name),
					Image: "alpine:latest",
					Command: []string{
						"kill",
					},
					Args: []string{
						fmt.Sprintf("-%d", st.sidecars[sidecar.Name]),
						"1",
					},
					ImagePullPolicy: v1.PullAlways,
				},
			})

			_, err := st.clientset.CoreV1().Pods(pod.Namespace).UpdateEphemeralContainers(context.TODO(), pod.Name, pod, metav1.UpdateOptions{})
			if err != nil {
				return err
			}
		}
	}

	return nil
}
