/*
Copyright 2016-2017 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package libvirttools

import (
	"fmt"

	"github.com/golang/glog"
	libvirt "github.com/libvirt/libvirt-go"
)

const (
	defaultCapacity     = 1024
	defaultCapacityUnit = "MB"
	poolTypeDir         = "dir"
)

type Volume struct {
	tool   StorageOperations
	Name   string
	volume *libvirt.StorageVol
}

func (v *Volume) Remove() error {
	return v.tool.RemoveVolume(v.volume)
}

func (v *Volume) GetPath() (string, error) {
	return v.tool.VolumeGetPath(v.volume)
}

type VolumeInfo struct {
	tool StorageOperations
	Name string
	Size uint64
}

func (v *Volume) Info() (*VolumeInfo, error) {
	return volumeInfo(v.tool, v.Name, v.volume)
}

func volumeInfo(tool StorageOperations, name string, volume *libvirt.StorageVol) (*VolumeInfo, error) {
	volInfo, err := tool.VolumeGetInfo(volume)
	if err != nil {
		return nil, err
	}
	return &VolumeInfo{Name: name, Size: volInfo.Capacity}, nil
}

type Pool struct {
	tool       StorageOperations
	pool       *libvirt.StoragePool
	volumesDir string
	poolType   string
}

type PoolSet map[string]*Pool

var DefaultPools PoolSet = PoolSet{
	"default": &Pool{volumesDir: "/var/lib/libvirt/images", poolType: poolTypeDir},
	"volumes": &Pool{volumesDir: "/var/lib/virtlet", poolType: poolTypeDir},
}

func generatePoolXML(name string, path string, poolType string) string {
	poolXML := `
<pool type="%s">
    <name>%s</name>
    <target>
	<path>%s</path>
    </target>
</pool>`
	return fmt.Sprintf(poolXML, poolType, name, path)
}

func createPool(tool StorageOperations, name string, path string, poolType string) (*Pool, error) {
	poolXML := generatePoolXML(name, path, poolType)

	glog.V(2).Infof("Creating storage pool (name: %s, path: %s)", name, path)
	pool, err := tool.CreateFromXML(poolXML)
	if err != nil {
		return nil, err
	}
	return &Pool{tool: tool, pool: pool, volumesDir: path, poolType: poolType}, nil
}

func LookupStoragePool(tool StorageOperations, name string) (*Pool, error) {
	poolInfo, exist := DefaultPools[name]
	if !exist {
		return nil, fmt.Errorf("pool with name '%s' is unknown", name)
	}

	pool, _ := tool.LookupByName(name)
	if pool == nil {
		return createPool(tool, name, poolInfo.volumesDir, poolInfo.poolType)
	}
	// TODO: reset libvirt error

	return &Pool{tool: tool, pool: pool, volumesDir: poolInfo.volumesDir, poolType: poolInfo.poolType}, nil
}

func (p *Pool) RemoveVolume(name string) error {
	vol, err := p.LookupVolume(name)
	if err != nil {
		return err
	}
	return vol.Remove()
}

func (p *Pool) CreateVolume(name, volXML string) (*Volume, error) {
	vol, err := p.tool.CreateVolFromXML(p.pool, volXML)
	if err != nil {
		return nil, err
	}
	return &Volume{tool: p.tool, Name: name, volume: vol}, nil
}

func (p *Pool) LookupVolume(name string) (*Volume, error) {
	vol, err := p.tool.LookupVolumeByName(p.pool, name)
	if err != nil {
		return nil, err
	}
	return &Volume{tool: p.tool, Name: name, volume: vol}, nil
}

func (p *Pool) ListVolumes() ([]*VolumeInfo, error) {
	volumes, err := p.tool.ListAllVolumes(p.pool)
	if err != nil {
		return nil, err
	}

	volumeInfos := make([]*VolumeInfo, 0, len(volumes))

	for _, volume := range volumes {
		name, err := p.tool.VolumeGetName(&volume)
		volInfo, err := volumeInfo(p.tool, name, &volume)
		if err != nil {
			return nil, err
		}

		volumeInfos = append(volumeInfos, volInfo)
	}

	return volumeInfos, nil
}

type StorageTool struct {
	name string
	tool StorageOperations
	pool *Pool
}

func NewStorageTool(conn *libvirt.Connect, poolName string) (*StorageTool, error) {
	tool := NewLibvirtStorageOperations(conn)
	pool, err := LookupStoragePool(tool, poolName)
	if err != nil {
		return nil, err
	}
	return &StorageTool{name: poolName, tool: tool, pool: pool}, nil
}

func (s *StorageTool) GenerateVolumeXML(shortName string, capacity int, capacityUnit string, path string) string {
	volXML := `
<volume>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
    <target>
        <path>%s</path>
    </target>
</volume>`
	return fmt.Sprintf(volXML, shortName, capacityUnit, capacity, path)
}

func (s *StorageTool) CreateVolume(name string, capacity int, capacityUnit string) (*Volume, error) {
	volumeXML := `
<volume>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
</volume>`
	volumeXML = fmt.Sprintf(volumeXML, name, capacityUnit, capacity)
	glog.V(2).Infof("Create volume using XML description: %s", volumeXML)
	return s.pool.CreateVolume(name, volumeXML)
}

func (s *StorageTool) CreateSnapshot(name string, capacity int, capacityUnit string, backingStorePath string) (*Volume, error) {
	snapshotXML := `
<volume type='file'>
    <name>%s</name>
    <allocation>0</allocation>
    <capacity unit="%s">%d</capacity>
    <target>
         <format type='qcow2'/>
    </target>
    <backingStore>
         <path>%s</path>
         <format type='qcow2'/>
     </backingStore>
</volume>`
	snapshotXML = fmt.Sprintf(snapshotXML, name, capacityUnit, capacity, backingStorePath)
	glog.V(2).Infof("Create volume using XML description: %s", snapshotXML)
	return s.pool.CreateVolume(name, snapshotXML)
}

func (s *StorageTool) LookupVolume(name string) (*Volume, error) {
	return s.pool.LookupVolume(name)
}

func (s *StorageTool) RemoveVolume(name string) error {
	return s.pool.RemoveVolume(name)
}

func (s *StorageTool) ListVolumes() ([]*VolumeInfo, error) {
	return s.pool.ListVolumes()
}

func (s *StorageTool) PullImageToVolume(path, volumeName string) error {
	libvirtFilePath := fmt.Sprintf("/var/lib/libvirt/images/%s", volumeName)
	volXML := s.GenerateVolumeXML(volumeName, 5, "G", libvirtFilePath)

	return s.tool.PullImageToVolume(s.pool.pool, volumeName, path, volXML)
}
