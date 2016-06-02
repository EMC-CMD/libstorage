package executor

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/akutz/gofig"
	"github.com/emccode/libstorage/api/registry"
	"github.com/emccode/libstorage/api/types"
	"github.com/emccode/libstorage/drivers/storage/vmax"
)

// driver is the storage executor for the VMAX storage driver.
type driver struct{}

func init() {
	registry.RegisterStorageExecutor(vmax.Name, newdriver)
}

func newdriver() types.StorageExecutor {
	return &driver{}
}

func (d *driver) Init(context types.Context, config gofig.Config) error {
	return nil
}

func (d *driver) Name() string {
	return vmax.Name
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
		Driver:    vmax.Name,
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

	return GetInstanceID()
}

// GetInstanceID returns the instance ID object
func GetInstanceID() (*types.InstanceID, error) {
	initiatorName, err := getiSCSIinitName()
	if err != nil {
		return nil, err
	}

	iid := &types.InstanceID{Driver: vmax.Name}
	if err := iid.MarshalMetadata(initiatorName); err != nil {
		return nil, err
	}

	return iid, nil
}

func getiSCSIinitName() (initiatorName string, err error) {
	file, err := os.Open("/etc/iscsi/initiatorname.iscsi")
	if err != nil {
		return "", fmt.Errorf("error reading /etc/iscsi/initiatorname.iscsi %s", err)
	}
	scanner := bufio.NewScanner(file)
	scanner.Split(bufio.ScanLines)
	var lastLine string
	for scanner.Scan() {
		lastLine = scanner.Text()
	}
	initiatorName = strings.Split(lastLine, "=")[1]
	if initiatorName == "" {
		return "", fmt.Errorf("error reading /etc/iscsi/initiatorname.iscsi")
	}

	return initiatorName, nil
}
