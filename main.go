package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
	"syscall"
	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const (
	socketAddress = "/run/docker/plugins/seaweedfs.sock"
)

type seaweedfsVolume struct {

	Name        string

	Host        string
	Filerpath   string

	Options     []string

	Mountpoint  string
	connections int
}

type seaweedfsDriver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*seaweedfsVolume
}

func newSeaweedfsDriver(root string) (*seaweedfsDriver, error) {
	logrus.WithField("method", "new driver").Debug(root)

	d := &seaweedfsDriver{
		root:      filepath.Join(root, "volumes"),
		statePath: filepath.Join(root, "state", "seaweedfs-state.json"),
		volumes:   map[string]*seaweedfsVolume{},
	}
	
	data, err := ioutil.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("statePath", d.statePath).Debug("no state found")
		} else {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &d.volumes); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *seaweedfsDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *seaweedfsDriver) Create(r *volume.CreateRequest) error {
	logrus.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &seaweedfsVolume{}

	v.Name = r.Name

	for key, val := range r.Options {
		switch key {
		case "host":
			v.Host = val
		case "filerpath":
			v.Filerpath = val
		default:
			if val != "" {
				v.Options = append(v.Options, key+"="+val)
			} else {
				v.Options = append(v.Options, key)
			}
		}
	}

	if v.Host == "" {
		return logError("'host' option required")
	}

 	if v.Filerpath == "" {
		return logError("'filerpath' option required")
	}
 
	v.Mountpoint = filepath.Join(d.root, fmt.Sprintf("%x", md5.Sum([]byte(v.Host + v.Filerpath))))

	d.volumes[r.Name] = v
	d.saveState()

	return nil
}

func (d *seaweedfsDriver) Remove(r *volume.RemoveRequest) error {
	logrus.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	if v.connections != 0 {
		return logError("volume %s is currently used by a container", r.Name)
	}

	if err := os.RemoveAll(v.Mountpoint); err != nil {
		return logError(err.Error())
	}

	delete(d.volumes, r.Name)
	d.saveState()

	return nil
}

func (d *seaweedfsDriver) Path(r *volume.PathRequest) (*volume.PathResponse, error) {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.PathResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.PathResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *seaweedfsDriver) Mount(r *volume.MountRequest) (*volume.MountResponse, error) {
	logrus.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.MountResponse{}, logError("volume %s not found", r.Name)
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.Mountpoint, 0755); err != nil {
				return &volume.MountResponse{}, logError(err.Error())
			}
		} else if err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return &volume.MountResponse{}, logError("%v already exists and is not a directory", v.Mountpoint)
		}

		if err := d.mountVolume(v); err != nil {
			return &volume.MountResponse{}, logError(err.Error())
		}
	}

	v.connections++

	return &volume.MountResponse{Mountpoint: v.Mountpoint}, nil
}

func (d *seaweedfsDriver) Unmount(r *volume.UnmountRequest) error {
	logrus.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return logError("volume %s not found", r.Name)
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v); err != nil {
			return logError(err.Error())
		}
		v.connections = 0
	}

	return nil
}

func (d *seaweedfsDriver) Get(r *volume.GetRequest) (*volume.GetResponse, error) {
	logrus.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return &volume.GetResponse{}, logError("volume %s not found", r.Name)
	}

	return &volume.GetResponse{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}, nil
}

func (d *seaweedfsDriver) List() (*volume.ListResponse, error) {
	logrus.WithField("method", "list").Debugf("")

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}

	return &volume.ListResponse{Volumes: vols}, nil
}

func (d *seaweedfsDriver) Capabilities() *volume.CapabilitiesResponse {
	logrus.WithField("method", "capabilities").Debugf("")

	return &volume.CapabilitiesResponse{Capabilities: volume.Capability{Scope: "local"}}
}



func (d *seaweedfsDriver) mountVolume(v *seaweedfsVolume) error {

	cmd := exec.Command("weed", "mount")

	cmd.Args = append(cmd.Args, "-dir=" + v.Mountpoint)

	if v.Host != "" {
		cmd.Args = append(cmd.Args, "-filer=" + v.Host)
	}

	if v.Filerpath != "" {
		cmd.Args = append(cmd.Args, "-filer.path=" + v.Filerpath)
	}

	for _, option := range v.Options {
		cmd.Args = append(cmd.Args, option)
	}

	logrus.Debug(cmd.Args)

	go func() {
		output, _ := cmd.CombinedOutput()
		logrus.Debug(output)
	}()
	
        var mounted bool
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		if mounted, err = isMounted(v.Mountpoint); mounted {
			return nil
		}
		logrus.Debugf("Error in attempt %d: %#v", attempt, err)
		time.Sleep(time.Duration(attempt)*time.Second)
	}
	return err
}

func isMounted(mountpoint string) (bool, error) {
    mntpoint, err := os.Stat(mountpoint)
    if err != nil {
        if os.IsNotExist(err) {
                return false, nil
        }
        return false, err
    }

    parent, err := os.Stat(filepath.Join(mountpoint, ".."))
    if err != nil {
        return false, err
    }

    mntpointSt := mntpoint.Sys().(*syscall.Stat_t)
    parentSt := parent.Sys().(*syscall.Stat_t)
    return mntpointSt.Dev != parentSt.Dev, nil
}

func (d *seaweedfsDriver) unmountVolume(v *seaweedfsVolume) error {
  if err := syscall.Unmount(v.Mountpoint, 0); err != nil {
		errno := err.(syscall.Errno)
		if errno == syscall.EINVAL {
			return logError("error unmounting invalid mount %s: %s", v.Mountpoint, err.Error())
		} else {
			return logError("error unmounting %s: %s", v.Mountpoint, err.Error())
		}
	}
	return nil
}

func logError(format string, args ...interface{}) error {
	logrus.Errorf(format, args...)
	return fmt.Errorf(format, args...)
}

func main() {
	debug := os.Getenv("DEBUG")
	if ok, _ := strconv.ParseBool(debug); ok {
		logrus.SetLevel(logrus.DebugLevel)
	}

	d, err := newSeaweedfsDriver("/mnt")
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	logrus.Infof("listening on %s", socketAddress)
	logrus.Error(h.ServeUnix(socketAddress, 0))
}
