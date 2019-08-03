// Copyright © 2019 Zachary Seguin <zachary@zacharyseguin.ca>
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/client-go/transport"
	"k8s.io/klog"

	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/zachomedia/kubernetes-sidecar-terminator/pkg/sidecarterminator"
)

// CLI settings
var kubeconfig string
var sidecars []string
var namespaces []string
var lockName string
var lockNamespace string
var lockUseConfigMap bool

var config *rest.Config
var client *clientset.Clientset

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "kubernetes-sidecar-terminator",
	Short: "Terminates sidecars on completed jobs",
	Long: `Monitors Pods created by Jobs and sends
a kill command to the sidecars once the job
has completed`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		klog.InitFlags(nil)
	},
	Run: func(cmd *cobra.Command, args []string) {
		// Load the Kubernetes configuration
		klog.Info("Hello, World!")
		klog.Infof("Kubeconfig: %q", kubeconfig)

		var err error
		config, err = buildConfig(kubeconfig)
		if err != nil {
			klog.Fatal(err)
		}

		client, err = clientset.NewForConfig(config)
		if err != nil {
			klog.Fatal(err)
		}

		// Start running the terminator
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Acquire a lock
		// Identity used to distinguish between multiple cloud controller manager instances
		id, err := os.Hostname()
		if err != nil {
			klog.Fatal(err)
		}
		// add a uniquifier so that two processes on the same host don't accidentally both become active
		id = id + "_" + string(uuid.NewUUID())
		klog.Infof("generated id: %s", id)

		var lock resourcelock.Interface

		if lockUseConfigMap {
			lock = &resourcelock.ConfigMapLock{
				ConfigMapMeta: metav1.ObjectMeta{
					Name:      lockName,
					Namespace: lockNamespace,
				},
				Client: client.CoreV1(),
				LockConfig: resourcelock.ResourceLockConfig{
					Identity: id,
				},
			}
		} else {
			lock = &resourcelock.LeaseLock{
				LeaseMeta: metav1.ObjectMeta{
					Name:      lockName,
					Namespace: lockNamespace,
				},
				Client: client.CoordinationV1(),
				LockConfig: resourcelock.ResourceLockConfig{
					Identity: id,
				},
			}
		}

		config.Wrap(transport.ContextCanceller(ctx, fmt.Errorf("the leader is shutting down")))

		wait := make(chan os.Signal, 1)
		signal.Notify(wait, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-wait
			klog.Info("received signal, shutting down")
			cancel()
		}()

		terminator, err := sidecarterminator.NewSidecarTerminator(config, client, sidecars, namespaces)
		if err != nil {
			klog.Fatal(err)
		}

		leaderelection.RunOrDie(ctx, leaderelection.LeaderElectionConfig{
			Lock:            lock,
			ReleaseOnCancel: true,
			LeaseDuration:   15 * time.Second,
			RenewDeadline:   10 * time.Second,
			RetryPeriod:     2 * time.Second,
			Callbacks: leaderelection.LeaderCallbacks{
				OnStartedLeading: func(ctx context.Context) {
					if err := terminator.Run(ctx); err != nil {
						if err != context.Canceled {
							klog.Errorf("error running terminator: %v", err)
						}
					}
				},
				OnStoppedLeading: func() {
					klog.Info("stopped leading")
				},
				OnNewLeader: func(identity string) {
					if identity == id {
						// We just acquired the lock
						return
					}

					klog.Infof("new leader elected: %v", identity)
				},
			},
		})

		klog.Info("done")
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.Flags().StringVar(&kubeconfig, "kubeconfig", "", "path to the kubeconfig file")
	rootCmd.Flags().StringArrayVar(&namespaces, "namespaces", []string{}, "namespaces to monitor (leave empty for all namespaces)")
	rootCmd.Flags().StringArrayVar(&sidecars, "sidecars", []string{"istio-proxy"}, "list of sidecar container names")

	// Lock info
	rootCmd.Flags().StringVar(&lockName, "lock-name", "sidecar-terminator", "name of the lock")
	rootCmd.Flags().StringVar(&lockNamespace, "lock-namespace", "kube-system", "namespace to create the lock")
	rootCmd.Flags().BoolVar(&lockUseConfigMap, "lock-use-config-map", false, "use a configmap instead of lock leases")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
