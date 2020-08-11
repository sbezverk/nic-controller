package controller

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
)

const (
	pidsBase    = "/sys/fs/cgroup/pids"
	cgroupProcs = "cgroup.procs"
)

// GetContainerPID returns PID of a container specified by its ID
func GetContainerPID(containerID string) (int, error) {
	p, err := getContainerCgroupProcs(pidsBase, containerID)
	if err != nil {
		return 0, err
	}
	if p == "" {
		return 0, fmt.Errorf("unable to find pid for container %s", containerID)
	}
	return strconv.Atoi(p)
}

func getContainerCgroupProcs(path string, containerID string) (string, error) {
	var processID string
	var err error
	files, err := ioutil.ReadDir(path)
	if err != nil {
		return "", err
	}
	for _, f := range files {
		if !f.IsDir() {
			continue
		}
		// It is directory, check if it contains containerID pattern
		if strings.Contains(f.Name(), containerID) {
			return getPIDFromCgroupProcs(path + "/" + f.Name())
		}
		processID, err = getContainerCgroupProcs(path+"/"+f.Name(), containerID)
		if err != nil {
			return "", err
		}
		if processID != "" {
			return processID, nil
		}
	}

	return processID, nil
}

func getPIDFromCgroupProcs(path string) (string, error) {
	b, err := ioutil.ReadFile(path + "/" + cgroupProcs)
	if err != nil {
		return "", err
	}
	bytes.Split(b, []byte{'\n'})

	return string(bytes.Split(b, []byte{'\n'})[0]), nil
}

func getContainerID(p *v1.Pod) (string, error) {
	if p.Status.Phase == v1.PodRunning {
		return strings.Split(p.Status.ContainerStatuses[0].ContainerID, "://")[1][:12], nil
	}
	if p.Status.Phase == v1.PodPending {
		// Check if we have Init containers
		if p.Status.InitContainerStatuses != nil {
			for _, i := range p.Status.InitContainerStatuses {
				if i.State.Running != nil {
					return strings.Split(i.ContainerID, "://")[1][:12], nil
				}
			}
		}
	}

	return "", fmt.Errorf("none of containers of pod %s/%s yet in running state", p.ObjectMeta.Namespace, p.ObjectMeta.Name)
}
