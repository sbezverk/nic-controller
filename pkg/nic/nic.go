package nic

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netns"
)

const (
	interfaceNameMaxLength = 15
)

func init() {
	runtime.LockOSThread()
}

// GetLink returns all network interfaces wuth names matching prefix
func GetLink(ns netns.NsHandle, prefix string) ([]netlink.Link, error) {
	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return nil, fmt.Errorf("failure to get pod's handle with error: %+v", err)
	}
	ls, err := h.LinkList()
	if err != nil {
		return nil, fmt.Errorf("failure to get pod's interfaces with error: %+v", err)
	}
	links := make([]netlink.Link, 0, len(ls))
	for _, link := range ls {
		if strings.HasPrefix(link.Attrs().Name, prefix) {
			links = append(links, link)
		}
	}

	return links, nil
}

// CheckLink returns true if the nic specified by the link's Name is found in the specified namespace
func CheckLink(ns netns.NsHandle, link netlink.Link) (bool, error) {
	h, err := netlink.NewHandleAt(ns)
	if err != nil {
		return false, fmt.Errorf("failure to get pod's handle with error: %+v", err)
	}
	ls, err := h.LinkList()
	if err != nil {
		return false, fmt.Errorf("failure to get pod's interfaces with error: %+v", err)
	}
	for _, l := range ls {
		if strings.Compare(link.Attrs().Name, l.Attrs().Name) == 0 {
			return true, nil
		}
	}

	return false, nil
}

// AllocateLink allocates a link to a specified destination namespace
func AllocateLink(destns netns.NsHandle, link netlink.Link) error {
	ns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get current process namespace")
	}
	defer netns.Set(ns)
	// Moving peer's interface into peer's namespace
	if err := netlink.LinkSetNsFd(link, int(destns)); err != nil {
		return fmt.Errorf("failure to place link into the destination namespace with error: %+v", err)
	}
	if err := waitForLink(destns, link); err != nil {
		return err
	}

	return nil
}

// DeallocateLink moves a link from a client namesspace backto a global namespace
func DeallocateLink(srcns, destns netns.NsHandle, link netlink.Link) error {
	ns, err := netns.Get()
	if err != nil {
		return fmt.Errorf("failed to get current process namespace")
	}
	defer netns.Set(ns)
	// Switch to ns where the link currently resides
	if err := netns.Set(srcns); err != nil {
		return fmt.Errorf("failed to set source namespace")
	}
	// Before moving the nic back, check if it is still there
	found, err := CheckLink(srcns, link)
	if err != nil {
		return fmt.Errorf("failed to check if link: %s exists in the source namespace", link.Attrs().Name)
	}
	if !found {
		return fmt.Errorf("link: %s has disappeared from the source namespace", link.Attrs().Name)
	}
	fmt.Printf("Link with name: %s has been found in the expected namespace\n", link.Attrs().Name)
	// Moving the link from source namespace to the destination namespace
	if err := netlink.LinkSetNsFd(link, int(destns)); err != nil {
		return fmt.Errorf("failure to place link into the destination namespace with error: %+v", err)
	}
	// Waiting for the link to appear in destination namespace
	if err := waitForLink(destns, link); err != nil {
		// For debugging purposes
		found, err := CheckLink(srcns, link)
		if err != nil {
			return fmt.Errorf("failed to check if link: %s exists in the source namespace After move", link.Attrs().Name)
		}
		if !found {
			return fmt.Errorf("link: %s has disappeared from the source namespace After move", link.Attrs().Name)
		}
		fmt.Printf("Link with name: %s has still been found in the source namespace After move\n", link.Attrs().Name)
		return err
	}

	return nil
}

func waitForLink(ns netns.NsHandle, link netlink.Link) error {
	org, err := netns.Get()
	if err != nil {
		return err
	}
	defer netns.Set(org)
	// Switch to ns where the link is expected to appear
	if err := netns.Set(ns); err != nil {
		return fmt.Errorf("failed to set source namespace")
	}
	nsh, err := netlink.NewHandleAt(ns)
	if err != nil {
		return fmt.Errorf("failure to get namespace's handle with error: %+v", err)
	}
	ticker := time.NewTicker(time.Millisecond * 250)
	timeout := time.NewTimer(time.Millisecond * 30000)
	for {
		links, _ := nsh.LinkList()
		for _, l := range links {
			if l.Attrs().Name == link.Attrs().Name {
				return netlink.LinkSetUp(link)
			}
		}
		select {
		case <-ticker.C:
			continue
		case <-timeout.C:
			return fmt.Errorf("timeout waiting for the link to appear in the namespace")
		}
	}
}
