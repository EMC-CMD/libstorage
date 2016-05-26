package executor

import (
	"fmt"
	"io/ioutil"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akutz/gofig"
	"github.com/akutz/goof"
	"github.com/emccode/libstorage/api/registry"
	"github.com/emccode/libstorage/api/types"
)

const (
	// Name is the name of the driver.
	Name = "vmax"
)

// driver is the storage executor for the VMAX storage driver.
type driver struct {
	Config     gofig.Config
	instanceID *types.InstanceID
}

func init() {
	registry.RegisterStorageExecutor(Name, newdriver)
}

func newdriver() types.StorageExecutor {
	return &driver{}
}

func (d *driver) Init(context types.Context, config gofig.Config) error {
	d.Config = config
	id, err := GetInstanceID()
	if err != nil {
		return err
	}
	d.instanceID = id
	return nil
}

func (d *driver) Name() string {
	return Name
}

// NextDevice returns the next available device.
func (d *driver) NextDevice(
	ctx types.Context,
	opts types.Store) (string, error) {
	return "", nil
}

// LocalDevices returns a map of the system's local devices.
func (d *driver) LocalDevices(
	ctx types.Context,
	opts *types.LocalDevicesOpts) (*types.LocalDevices, error) {

	lvm, err := getLocalWWNDeviceByID()
	if err != nil {
		return nil, err
	}

	return &types.LocalDevices{
		Driver:    Name,
		DeviceMap: lvm,
	}, nil
}

func deviceMapper() bool {
	return false
}

func multipath() bool {
	return false
}

func getLocalWWNDeviceByID() (map[string]string, error) {
	mapDiskByID := make(map[string]string)
	diskIDPath := "/dev/disk/by-id"
	files, err := ioutil.ReadDir(diskIDPath)
	if err != nil {
		return nil, err
	}

	var match1 *regexp.Regexp
	var match2 string

	if deviceMapper() || multipath() {
		match1, _ = regexp.Compile(`^dm-name-\w*$`)
		match2 = `^dm-name-\d+`
	} else {
		match1, _ = regexp.Compile(`^wwn-0x\w*$`)
		match2 = `^wwn-0x`
	}

	for _, f := range files {
		if match1.MatchString(f.Name()) {
			naaName := strings.Replace(f.Name(), match2, "", 1)
			//32 for WWN
			naaName = naaName[len(naaName)-32:]
			devPath, _ := filepath.EvalSymlinks(fmt.Sprintf("%s/%s", diskIDPath, f.Name()))
			mapDiskByID[naaName] = devPath
		}
	}
	return mapDiskByID, nil
}

// InstanceID returns the local system's InstanceID.
func (d *driver) InstanceID(
	ctx types.Context,
	opts types.Store) (*types.InstanceID, error) {
	return d.instanceID, nil
}

// GetInstanceID returns the instance ID object
func GetInstanceID() (*types.InstanceID, error) {
	mac, err := getLocalMACAddress()
	if err != nil {
		return nil, err
	}
	iid := &types.InstanceID{Driver: Name}
	if err := iid.MarshalMetadata(mac); err != nil {
		return nil, err
	}

	return iid, nil
}

func getLocalMACAddress() (macAddress string, err error) {
	out, err := exec.Command("cat", "cat /sys/class/net/eth0/address").Output()
	if err != nil {
		return "", goof.WithError("problem getting mac address", err)
	}

	macAddress = strings.Replace(string(out), "\n", "", -1)

	return macAddress, nil
}
