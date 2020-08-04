package controller

import (
	"fmt"
	"sync"

	"github.com/sbezverk/nic-controller/pkg/nic"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1informer "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const podControllerAgentName = "nic-controller"

// PodController defines interface for managing pod controller
type PodController interface {
	Start(<-chan struct{}) error
}

type clientInfo struct {
	namespace netns.NsHandle
	link      netlink.Link
}

type linkState uint8

const (
	linkAvailable linkState = iota
	linkAllocated
)

type podController struct {
	namespace     netns.NsHandle
	localNodeIP   string
	kubeClientset kubernetes.Interface
	podsSynced    cache.InformerSynced
	sync.Mutex
	clients  map[types.UID]*clientInfo
	linkPool map[netlink.Link]linkState
}

func (c *podController) addClient(p *v1.Pod) {
	klog.V(5).Infof("pod Controller add client for pod: %s/%s", p.ObjectMeta.Namespace, p.ObjectMeta.Name)
	c.Lock()
	defer c.Unlock()
	if _, ok := c.clients[p.ObjectMeta.UID]; ok {
		klog.V(5).Infof("pod Controller pod: %s/%s already exists", p.ObjectMeta.Namespace, p.ObjectMeta.Name)
		// The client has already been recorded in the map
		return
	}
	id, err := getContainerID(p)
	if err != nil {
		klog.Errorf("pod Controller failed to get Container ID for pod: %s/%s with error: %+v", p.ObjectMeta.Namespace, p.ObjectMeta.Name, err)
		return
	}
	klog.V(5).Infof("container id: %+s", id)
	pid, err := GetContainerPID(id)
	if err != nil {
		klog.Errorf("fail to get container's pid with error: %+v", err)
		return
	}
	ns, err := netns.GetFromPid(pid)
	if err != nil {
		klog.Errorf("fail to get pod's linux namespace with error: %+v", err)
		return
	}
	klog.V(5).Infof("container's linux namespace: %+v", ns)
	link, err := c.getAvailableLink()
	if err != nil {
		klog.Errorf("fail to get available link with error: %+v", err)
		return
	}
	if err := nic.AllocateLink(ns, link); err != nil {
		klog.Errorf("fail to allocate link with error: %+v", err)
		return
	}
	c.linkPool[link] = linkAllocated
	c.clients[p.ObjectMeta.UID] = &clientInfo{
		namespace: ns,
		link:      link,
	}
	klog.V(5).Infof("Client's namespace %+v", ns)
}

func (c *podController) removeClient(p *v1.Pod) {
	c.Lock()
	defer c.Unlock()
	cl, ok := c.clients[p.ObjectMeta.UID]
	if !ok {
		// The client has already been recorded in the map
		return
	}
	// Returning link from the client's namespace back to the controller's namespace
	if err := nic.DeallocateLink(cl.namespace, c.namespace, cl.link); err != nil {
		klog.Errorf("fail to deallocate link with error: %+v", err)
	}
	c.linkPool[cl.link] = linkAvailable
	delete(c.clients, p.ObjectMeta.UID)
}

func (c *podController) getAvailableLink() (netlink.Link, error) {
	for l, s := range c.linkPool {
		if s == linkAvailable {
			return l, nil
		}
	}

	return nil, fmt.Errorf("no available link found")
}

func (c *podController) Start(stopCh <-chan struct{}) error {
	// Start the informer factories to begin populating the informer caches
	klog.Infof("Starting nic controller on %s", c.localNodeIP)

	// Wait for the caches to be synced before starting workers
	klog.Info("Waiting for Informer caches to sync")
	if ok := cache.WaitForCacheSync(stopCh, c.podsSynced); !ok {
		return fmt.Errorf("failed to wait for caches to sync for Service controller")
	}
	klog.Info("Informer's caches has synced")

	return nil
}

// NewPodController returns a new serices controller wathing and calling nic methods
// for pod add/delete/update events.
func NewPodController(kubeClientset kubernetes.Interface, podInformer corev1informer.PodInformer, localNodeIP string, ns netns.NsHandle, links []netlink.Link) PodController {
	controller := &podController{
		kubeClientset: kubeClientset,
		podsSynced:    podInformer.Informer().HasSynced,
		localNodeIP:   localNodeIP,
		namespace:     ns,
		clients:       make(map[types.UID]*clientInfo),
	}
	controller.linkPool = make(map[netlink.Link]linkState, len(links))
	for _, l := range links {
		controller.linkPool[l] = linkAvailable
	}
	// Set up an event handler for when Svc resources change
	podInformer.Informer().AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    controller.handleAddPod,
		UpdateFunc: controller.handleUpdatePod,
		DeleteFunc: controller.handleDeletePod,
	})

	return controller
}
