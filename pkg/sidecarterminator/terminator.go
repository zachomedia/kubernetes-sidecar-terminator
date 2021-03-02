package sidecarterminator

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"syscall"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/klog"
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
	var err error

	klog.Infof("Terminating running sidecar containers from %s", podName(pod))

	// Terminate the sidecar
	for _, sidecar := range pod.Status.ContainerStatuses {
		if isSidecarContainer(sidecar.Name, st.sidecars) && sidecar.State.Running != nil {
			klog.Infof("Terminating sidecar %s from %s with signal %d", sidecar.Name, podName(pod), st.sidecars[sidecar.Name])

			err = st.terminateSidecarCommand(pod, sidecar.Name, []string{"/bin/kill", "-s", strconv.Itoa(st.sidecars[sidecar.Name]), "1"})
			if err != nil {
				// Try the shell version
				klog.Errorf("Failed to terminate: %v", err)
				klog.Warningf("Failed using using standard approach.. trying shell approach")

				err = st.terminateSidecarCommand(pod, sidecar.Name, []string{"/bin/sh", "-c", fmt.Sprintf("kill -s \"%s\" 1", strconv.Itoa(st.sidecars[sidecar.Name]))})
				if err != nil {
					klog.Errorf("Failed to terminate: %v", err)
				}
			}
		}
	}

	return nil
}

func (st *SidecarTerminator) terminateSidecarCommand(pod *v1.Pod, sidecar string, command []string) error {
	req := st.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Namespace(pod.Namespace).
		Name(pod.Name).
		SubResource("exec")
	scheme := runtime.NewScheme()

	if err := v1.AddToScheme(scheme); err != nil {
		return err
	}

	parameterCodec := runtime.NewParameterCodec(scheme)
	req.VersionedParams(&v1.PodExecOptions{
		Command:   command,
		Container: sidecar,
		Stdin:     false,
		Stdout:    true,
		Stderr:    false,
		TTY:       false,
	}, parameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(st.config, "POST", req.URL())
	if err != nil {
		klog.Errorf("Failed to execute: %s", err)
	}
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: ioutil.Discard,
		Tty:    false,
	})
	if err != nil {
		klog.Errorf("Failed to execute: %s", err)
	}
	return err
}
