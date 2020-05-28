package controller

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

func (c *podController) handleAddPod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
		return
	}
	klog.V(5).Infof("pod add event for %s/%s", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
	if pod.Status.HostIP == "" {
		// Pod has not been yet picked by some node's kubelet
		return
	}
	if strings.Compare(c.localNodeIP, pod.Status.HostIP) != 0 {
		klog.V(5).Infof("pod runs on host: %s controller runs on host: %s hence pod is not local, ignoring it", pod.Status.HostIP, c.localNodeIP)
		return
	}
	c.addClient(pod)
}

func (c *podController) handleUpdatePod(oldObj, newObj interface{}) {
	podOld, ok := oldObj.(*v1.Pod)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", oldObj))
		return
	}
	podNew, ok := newObj.(*v1.Pod)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", newObj))
		return
	}
	if podOld.ObjectMeta.ResourceVersion == podNew.ObjectMeta.ResourceVersion {
		return
	}
	if podNew.Status.HostIP == "" {
		// Pod has not been yet picked by some node's kubelet
		return
	}
	klog.V(5).Infof("pod update event for %s/%s", podNew.ObjectMeta.Namespace, podNew.ObjectMeta.Name)
	if strings.Compare(c.localNodeIP, podNew.Status.HostIP) != 0 {
		klog.V(5).Infof("pod runs on host: %s controller runs on host: %s hence pod is not local, ignoring it", podNew.Status.HostIP, c.localNodeIP)
		return
	}
	if podOld.DeletionTimestamp == nil && podNew.DeletionTimestamp != nil {
		klog.V(5).Infof("pod %s/%s is about to be deleted", podNew.ObjectMeta.Namespace, podNew.ObjectMeta.Name)
		//		c.removeClient(podNew)
	} else {
		c.addClient(podNew)
	}
}

func (c *podController) handleDeletePod(obj interface{}) {
	pod, ok := obj.(*v1.Pod)
	if !ok {
		tombstone, ok := obj.(cache.DeletedFinalStateUnknown)
		if !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
		if _, ok = tombstone.Obj.(*v1.Pod); !ok {
			utilruntime.HandleError(fmt.Errorf("unexpected object type: %v", obj))
			return
		}
	}
	klog.V(5).Infof("pod delete event for %s/%s", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name)
	if pod.Status.HostIP == "" {
		// Pod has not been yet picked by some node's kubelet
		return
	}
	if strings.Compare(c.localNodeIP, pod.Status.HostIP) != 0 {
		klog.V(5).Infof("pod runs on host: %s controller runs on host: %s hence pod is not local, ignoring it", pod.Status.HostIP, c.localNodeIP)
		return
	}
	c.removeClient(pod)
}
