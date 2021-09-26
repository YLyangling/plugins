package main

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/containernetworking/plugins/plugins/ipam/host-local/backend/disk"
)

var defaultDataDir = "/var/lib/cni/networks"

// Store is a simple disk-backed store that creates one file per mac_MAC
// address in a given directory.
type Store struct {
	*disk.FileLock
	dataDir string
}

func New(network, dataDir string) (*Store, error) {
	if dataDir == "" {
		dataDir = defaultDataDir
	}
	dir := filepath.Join(dataDir, network)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	lk, err := disk.NewFileLock(dir)
	if err != nil {
		return nil, err
	}
	return &Store{lk, dir}, nil
}

// edge k8s: hasReservedMac verify the pod already had reserved MAC or not.
// and return the reserved ip on the other hand.
func (s *Store) hasReservedMac(podNs, podName string) (net.HardwareAddr, error) {
	if len(podName) == 0 {
		return nil, nil
	}

	// Pod, mac mapping info are recorded with file name: mac_PodMac_PodNs_PodName
	podMacNsNameFileName, err := s.findPodFileName("", podNs, podName)
	if err != nil {
		return nil, err
	}

	if len(podMacNsNameFileName) != 0 {
		mac, ns, name := resolvePodFileName(podMacNsNameFileName)
		if ns == podNs && name == podName {
			return net.ParseMAC(mac)
		}
	}

	return nil, nil
}

func podFileName(mac, ns, name string) string {
	if len(mac) != 0 && len(ns) != 0 {
		return fmt.Sprintf("mac_%s_%s_%s", mac, ns, name)
	}

	return name
}

// mac_podMac_podNs_podName
func resolvePodFileName(fName string) (mac, ns, name string) {
	parts := strings.Split(fName, "_")
	if len(parts) == 4 {
		mac = parts[1]
		ns = parts[2]
		name = parts[3]
	}

	return
}

func (s *Store) findPodFileName(mac, ns, name string) (string, error) {
	var pattern string
	if len(mac) != 0 {
		pattern = fmt.Sprintf("mac_%s_*", mac)
	} else if len(ns) != 0 && len(name) != 0 {
		pattern = fmt.Sprintf("mac_*_%s_%s", ns, name)
	} else {
		return "", nil
	}
	pattern = disk.GetEscapedPath(s.dataDir, pattern)

	podFiles, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(podFiles) == 1 {
		_, fName := filepath.Split(podFiles[0])
		if strings.Count(fName, "_") == 3 {
			return fName, nil
		}
	}

	return "", nil
}

// edge k8s: reservePodInfo create podName file for storing mac
// in terms of podMacIsExist
func (s *Store) reservePodInfo(mac, podNs, podName string, podMacIsExist bool) (bool, error) {
	if !podMacIsExist && len(podName) != 0 {
		// for new pod, create a new file named "mac_PodMac_PodNs_PodName",
		// if there is already file named with prefix "mac_PodMac", rename the old file with new PodNs and PodName.
		podMacNsNameFile := disk.GetEscapedPath(s.dataDir, podFileName(mac, podNs, podName))
		podMacNsNameFileName, err := s.findPodFileName(mac, "", "")
		if err != nil {
			return false, err
		}

		if len(podMacNsNameFileName) != 0 {
			oldPodIPNsNameFile := disk.GetEscapedPath(s.dataDir, podMacNsNameFileName)
			err = os.Rename(oldPodIPNsNameFile, podMacNsNameFile)
			if err != nil {
				return false, err
			} else {
				return true, nil
			}
		}

		err = ioutil.WriteFile(podMacNsNameFile, []byte{}, 0644)
		if err != nil {
			return false, err
		}
	}

	return true, nil
}

func (s *Store) GetContainerMac(podNs, podName string) (bool, string, error) {
	s.Lock()
	defer s.Unlock()

	hw, err := s.hasReservedMac(podNs, podName)
	if hw == nil || err != nil {
		return false, "", err
	}
	return true, hw.String(), nil
}

func (s *Store) SaveContainerMac(mac, podNs, podName string, podMacIsExist bool) error {
	s.Lock()
	defer s.Unlock()

	_, err := s.reservePodInfo(mac, podNs, podName, podMacIsExist)

	return err
}
