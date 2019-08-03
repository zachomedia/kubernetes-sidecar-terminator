module github.com/zachomedia/kubernetes-sidecar-terminator

go 1.12

require (
	github.com/mitchellh/go-homedir v1.1.0
	github.com/spf13/cobra v0.0.5
	github.com/spf13/viper v1.4.0
	k8s.io/api v0.0.0
	k8s.io/apimachinery v0.0.0
	k8s.io/client-go v0.0.0-20190802021151-fdb3fbe99e1d
	k8s.io/klog v0.3.3
)

replace k8s.io/api => github.com/zachomedia/kubernetes/staging/src/k8s.io/api v0.0.0-20190803214003-07b7446ae671

replace k8s.io/client-go => github.com/zachomedia/kubernetes/staging/src/k8s.io/client-go v0.0.0-20190803214003-07b7446ae671

replace k8s.io/apimachinery => github.com/zachomedia/kubernetes/staging/src/k8s.io/apimachinery v0.0.0-20190803214003-07b7446ae671
