package main

import (
	"context"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	_ "net/http/pprof"

	"github.com/sbezverk/nic-controller/pkg/controller"
	"github.com/sbezverk/nic-controller/pkg/nic"
	"github.com/vishvananda/netns"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/component-base/logs"
	"k8s.io/klog"
	utilnode "k8s.io/kubernetes/pkg/util/node"
)

const (
	jalapenoInfraLabel      = "jalapeno.io/infra-app"
	jalapenoInfraLabelValue = "vpp-forwarder"
)

var (
	kubeconfig string
	portPrefix string
)

func init() {
	runtime.LockOSThread()
	flag.StringVar(&kubeconfig, "kubeconfig", "", "Absolute path to the kubeconfig file.")
	flag.StringVar(&portPrefix, "tor-interface-prefix", "tor_vlan", "The name of the interface connected to top of rack switch.")
}

func main() {
	flag.Parse()
	_ = flag.Set("logtostderr", "true")
	rand.Seed(time.Now().UnixNano())
	logs.InitLogs()
	defer logs.FlushLogs()

	// Getting current Linuc netowork namespace
	ns, err := netns.Get()
	if err != nil {
		klog.Errorf("Failed to get current linux net namespace with error: %+v", err)
		os.Exit(1)
	}
	klog.V(5).Infof("nic controller namespace: %+v", ns)
	links, err := nic.GetLink(ns, portPrefix)
	if err != nil {
		klog.Errorf("Failed to get host's network interfaces with error: %+v", err)
		os.Exit(1)
	}
	if len(links) == 0 {
		klog.Warningf("no network interfaces with prefix: %s found", portPrefix)
	} else {
		klog.V(5).Infof("nic controller discovered %d interfaces with prefix %s", len(links), portPrefix)
	}
	// Get kubernetes client set
	client, err := controller.GetClientset(kubeconfig)
	if err != nil {
		klog.Errorf("nic controller failed to get kubernetes clientset with error: %+v", err)
		os.Exit(1)
	}

	hostIPStr, err := getCurrentNodeIP(client)
	if err != nil {
		klog.Errorf("%+v", err)
		os.Exit(1)
	}
	labelSelector := labels.NewSelector()
	podSelector, err := labels.NewRequirement(jalapenoInfraLabel, selection.DoubleEquals, []string{jalapenoInfraLabelValue})
	if err != nil {
		klog.Errorf("Failed to create Requirement for %s label with error: %+v", jalapenoInfraLabel, err)
		os.Exit(1)
	}
	labelSelector = labelSelector.Add(*podSelector)

	kubeInformerFactory := kubeinformers.NewSharedInformerFactoryWithOptions(client, time.Minute*10,
		kubeinformers.WithTweakListOptions(func(options *metav1.ListOptions) {
			options.LabelSelector = labelSelector.String()
		}))
	podController := controller.NewPodController(client, kubeInformerFactory.Core().V1().Pods(), hostIPStr, ns, links)
	kubeInformerFactory.Start(wait.NeverStop)
	if err = podController.Start(wait.NeverStop); err != nil {
		klog.Fatalf("Error running pod controller: %s", err.Error())
	}

	stopCh := setupSignalHandler()
	<-stopCh
	klog.Info("Received stop signal, shuting down controller")

	os.Exit(0)
}

func getCurrentNodeIP(client *kubernetes.Clientset) (string, error) {
	hostname, err := utilnode.GetHostname("")
	if err != nil {
		return "", fmt.Errorf("nic controller failed to get local host name with error: %+v", err)
	}
	nodes, err := client.CoreV1().Nodes().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return "", fmt.Errorf("nic controller failed to get list of nodes with error: %+v", err)
	}
	found := false
	for _, n := range nodes.Items {
		for _, a := range n.Status.Addresses {
			if a.Type == v1.NodeExternalIP || a.Type == v1.NodeInternalIP {
				continue
			}
			if strings.Contains(a.Address, hostname) {
				found = true
				break
			}
		}
		if !found {
			continue
		}
		var addr string
		for _, a := range n.Status.Addresses {
			switch a.Type {
			case v1.NodeInternalIP:
				return a.Address, nil
			case v1.NodeExternalIP:
				addr = a.Address
			}
		}
		return addr, nil
	}

	return "", fmt.Errorf("failed to get IP address of a current node")
}

func setupSignalHandler() (stopCh <-chan struct{}) {
	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return stop
}
