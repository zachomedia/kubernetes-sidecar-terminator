## Why does this exist?

Currently there is an issue when using sidecars (like istio-proxy)
with jobs - they don't exit when the job has completed.

This project will monitor those pods and then send a `kill` signal to the
sidecar containers causing them to exit and the job to be marked
as succeeded.

## How does it work?

It will monitor all pods in the cluster, and terminate the sidecars
if the following criteria has been met:

1. The pod was created by a job
2. The pod's non-sidecar containers have exited with exit code 0

Terminate occurs by creating an (ephemeral container)[https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/]
attached to the sidecar's process namespace and running `kill 1`.

## Limitations

Currently, this implementation cannot handle
pods which leverage (`shareProcessNamespace`)[https://kubernetes.io/docs/tasks/configure-pod-container/share-process-namespace/].

## Getting started

1. `git clone https://github.com/zachomedia/kubernetes-sidecar-terminator.git`
2. `kubectl apply -f manifests/`
