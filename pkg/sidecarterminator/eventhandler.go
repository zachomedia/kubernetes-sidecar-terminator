package sidecarterminator

import (
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog"
)

type sidecarTerminatorEventHandler struct {
	st *SidecarTerminator
}

func (steh *sidecarTerminatorEventHandler) OnAdd(obj interface{}) {
	switch obj := obj.(type) {
	case *v1.Pod:
		steh.handle(obj)
	}
}

func (steh *sidecarTerminatorEventHandler) OnUpdate(oldObj, newObj interface{}) {
	switch obj := newObj.(type) {
	case *v1.Pod:
		steh.handle(obj)
	}
}

func (steh *sidecarTerminatorEventHandler) OnDelete(obj interface{}) {}

func (steh *sidecarTerminatorEventHandler) handle(pod *v1.Pod) error {
	if isOwnedByJob(pod.OwnerReferences) {
		if isCompleted(pod, steh.st.sidecars) {
			err := steh.st.terminate(pod)
			if err != nil {
				if k8serrors.IsNotFound(err) {
					klog.Infof("%q not found", podName(pod))
				} else {
					klog.Infof("Error terminating %q: %s", podName(pod), err)
					return err
				}
			}
		}
	}

	return nil
}
