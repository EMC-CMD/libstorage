package storage

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/akutz/gofig"
	"github.com/akutz/goof"

	symm "github.com/EMC-CMD/govmaxapi/api"
	symmtypes "github.com/EMC-CMD/govmaxapi/model"

	"github.com/emccode/libstorage/api/context"
	"github.com/emccode/libstorage/api/registry"
	"github.com/emccode/libstorage/api/types"
	"github.com/emccode/libstorage/drivers/storage/vmax"
)

type driver struct {
	config   gofig.Config
	client   *symm.Client
	systemID string
}

func init() {
	registry.RegisterStorageDriver(vmax.Name, newDriver)
}

func newDriver() types.StorageDriver {
	return &driver{}
}

func (d *driver) Name() string {
	return vmax.Name
}

func (d *driver) Init(context types.Context, config gofig.Config) error {
	d.systemID = d.systemID()
	d.config = config
	fields := eff(map[string]interface{}{
		"endpoint": d.endpoint(),
	})

	log.WithFields(fields).Debug("starting vmax driver")

	var err error
	if d.client, err = vmax.NewClient(
		d.endpoint(),
		d.userName(),
		d.password(),
		d.port(),
		d.insecure()); err != nil {
		return goof.WithFieldsE(fields, "error constructing new client", err)
	}

	log.WithFields(fields).Info("storage initialized")
	return nil
}

func (d *driver) Type(ctx types.Context) (types.StorageType, error) {
	return types.Block, nil
}

func (d *driver) InstanceInspect(ctx types.Context, opts types.Store) (*types.Instance, error) {
	iid := context.MustInstanceID(ctx)
	if iid.ID != "" {
		return &types.Instance{InstanceID: iid}, nil
	}

	var systemID *vmax.ID
	if err := iid.UnmarshalMetadata(&systemID); err != nil {
		return nil, err
	}

	if system, err = d.system.FindSystemByID(systemID); err != nil {
		return nil, fmt.Errorf("unable to find system id")
	}

	if system != nil {
		return &types.Instance{
			InstanceID: &types.InstanceID{
				ID: systemID,
			},
		}, nil
	}

	return nil, fmt.Errorf("system id is not set in config")
}

func (d *driver) Volumes(
	ctx types.Context,
	opts *types.VolumesOpts) (volumes []*types.Volume, err error) {

	vmaxVolumes, err := d.client.ListVolumes(d.systemID)
	if err != nil {
		return []*types.Volume{}, err
	}

	for _, v := range vmaxVolumes.ResultList.Result {
		singleVol, err := d.client.GetVolume(d.systemID, v.VolumeID)
		if err != nil {
			return []*types.Volume{}, err
		}
		volume := &types.Volume{
			Name:   singleVol.Volume[0].VolumeIdentifier,
			ID:     v.VolumeID,
			Status: "",
			Type:   singleVol.Volume[0].Type,
			Size:   int64(singleVol.Volume[0].CapGb),
		}
		volumes = append(volumes, volume)
	}

	return volumes, nil
}

func (d *driver) VolumeInspect(
	ctx types.Context,
	volumeID string,
	opts *types.VolumeInspectOpts) (*types.Volume, error) {
	if volumeID == "" {
		return nil, goof.New("no volumeID specified")
	}

	vmaxVolume, err := d.client.GetVolume(d.systemID, volumeID)
	if err != nil {
		return nil, err
	}
	// if len(volumes) == 0 {
	// 	return nil, nil
	// }

	volume := &types.Volume{
		Name:   vmaxVolume.Volume[0].VolumeIdentifier,
		ID:     vmaxVolume.Volume[0].VolumeID,
		Status: "",
		Type:   vmaxVolume.Volume[0].Type,
		Size:   int64(vmaxVolume.Volume[0].CapGb),
	}

	return volume, nil
}

func (d *driver) VolumeCreate(ctx types.Context, volumeName string,
	opts *types.VolumeCreateOpts) (*types.Volume, error) {
	fields := eff(map[string]interface{}{
		"volumeName": volumeName,
		"opts":       opts,
	})
	log.WithFields(fields).Debug("creating volume")

	volume := &types.Volume{}
	if opts.AvailabilityZone != nil {
		volume.AvailabilityZone = *opts.AvailabilityZone
	}
	if opts.Type != nil {
		volume.Type = *opts.Type
	}
	if opts.Size != nil {
		volume.Size = *opts.Size
	}

	vmaxVolume := symmtypes.EditStorageGroupParam{
		EditStorageGroupActionParam: symmtypes.EditStorageGroupActionParam{
			ExpandStorageGroupParam: symmtypes.ExpandStorageGroupParam{
				NumOfVols: 1,
				VolumeAttribute: symmtype.VolumeAttribute{
					CapacityUnit: "GB",
					VolumeSize:   strconv.FormatInt(volume.Size, 10),
				},
				CreateNewVolumes: true,
				Emulation:        "FBA",
			},
		},
	}

	err := d.client.CreateVolume(d.systemID, "EMCDOJO", vmaxVolume)
	if err != nil {
		return nil, fmt.Errorf("unable to create volume: %s", err)
	}

	return volume, nil
}

func (d *driver) VolumeCreateFromSnapshot(
	ctx types.Context,
	snapshotID, volumeName string,
	opts *types.VolumeCreateOpts) (*types.Volume, error) {
	return nil, nil
}

func (d *driver) VolumeCopy(
	ctx types.Context,
	volumeID, volumeName string,
	opts types.Store) (*types.Volume, error) {
	return nil, nil
}

func (d *driver) VolumeSnapshot(
	ctx types.Context,
	volumeID, snapshotName string,
	opts types.Store) (*types.Snapshot, error) {
	return nil, nil
}

func (d *driver) VolumeRemove(
	ctx types.Context,
	volumeID string,
	opts types.Store) error {

	fields := eff(map[string]interface{}{
		"volumeId": volumeID,
	})

	if err := d.client.DeleteVolume(d.systemID, "EMCDOJO", volumeID); err != nil {
		return goof.WithFieldsE(fields, "error removing volume", err)
	}

	log.WithFields(fields).Debug("removed volume")
	return nil
}

func (d *driver) VolumeAttach(
	ctx types.Context,
	volumeID string,
	opts *types.VolumeAttachOpts) (*types.Volume, string, error) {

	if err := d.client.DeleteVolumeFromStorageGroup(d.systemID, "EMCDOJO", volumeID); err != nil {
		return goof.WithFieldsE(fields, "error removing volume from EMCDOJO SG", err)
	}

	addVolumeParam := symmtypes.EditStorageGroupParam{
		EditStorageGroupActionParam: symmtypes.EditStorageGroupActionParam{
			AddVolumeParam: symmtypes.AddVolumeParam{
				VolumeID: []string{
					volumeID,
				},
			},
		},
	}

	if err := d.client.EditStorageGroup(symmetricID, "CloudFoundry", addVolumeParam); err != nil {
		return goof.WithFieldsE(fields, "error adding volume to CloudFoundry SG", err)
	}

	attachedVol, err := d.VolumeInspect(
		ctx, volumeID, &types.VolumeInspectOpts{
			Attachments: true,
			Opts:        opts.Opts,
		})
	if err != nil {
		return nil, "", goof.WithError("error getting volume after adding to CloudFoundry SG", err)
	}

	return attachedVol, attachedVol.ID, nil
}

func (d *driver) VolumeDetach(
	ctx types.Context,
	volumeID string,
	opts *types.VolumeDetachOpts) (*types.Volume, error) {

	if err := d.client.DeleteVolumeFromStorageGroup(d.systemID, "CloudFoundry", volumeID); err != nil {
		return goof.WithFieldsE(fields, "error removing volume from CloudFoundry SG", err)
	}

	addVolumeParam := symmtypes.EditStorageGroupParam{
		EditStorageGroupActionParam: symmtypes.EditStorageGroupActionParam{
			AddVolumeParam: symmtypes.AddVolumeParam{
				VolumeID: []string{
					volumeID,
				},
			},
		},
	}

	if err := d.client.EditStorageGroup(symmetricID, "EMCDOJO", addVolumeParam); err != nil {
		return goof.WithFieldsE(fields, "error adding volume to EMCDOJO SG", err)
	}

	attachedVol, err := d.VolumeInspect(
		ctx, volumeID, &types.VolumeInspectOpts{
			Attachments: true,
			Opts:        opts.Opts,
		})
	if err != nil {
		return nil, "", goof.WithError("error getting volume after adding to EMCDOJO SG", err)
	}

	return volume, nil
}

func (d *driver) NextDeviceInfo(ctx types.Context) (*types.NextDeviceInfo, error) {
	return nil, nil
}

func (d *driver) VolumeDetachAll(
	ctx types.Context,
	volumeID string,
	opts types.Store) error {
	return nil
}

func (d *driver) Snapshots(
	ctx types.Context,
	opts types.Store) ([]*types.Snapshot, error) {
	return nil, nil
}

func (d *driver) SnapshotInspect(
	ctx types.Context,
	snapshotID string,
	opts types.Store) (*types.Snapshot, error) {
	return nil, nil
}

func (d *driver) SnapshotCopy(
	ctx types.Context,
	snapshotID, snapshotName, destinationID string,
	opts types.Store) (*types.Snapshot, error) {
	return nil, nil
}

func (d *driver) SnapshotRemove(
	ctx types.Context,
	snapshotID string,
	opts types.Store) error {
	return nil
}

///////////////////////////////////////////////////////////////////////
////// HELPER FUNCTIONS FOR VMAX DRIVER FROM THIS POINT ON /////////
///////////////////////////////////////////////////////////////////////

func eff(fields goof.Fields) map[string]interface{} {
	errFields := map[string]interface{}{
		"provider": "scaleIO",
	}
	if fields != nil {
		for k, v := range fields {
			errFields[k] = v
		}
	}
	return errFields
}

///////////////////////////////////////////////////////////////////////
//////                  CONFIG HELPER STUFF                   /////////
///////////////////////////////////////////////////////////////////////

func getLocalMACAddress() (macAddress string, err error) {
	out, err := exec.Command("cat", "cat /sys/class/net/eth0/address").Output()
	if err != nil {
		return "", goof.WithError("problem getting mac address", err)
	}

	macAddress = strings.Replace(string(out), "\n", "", -1)

	return macAddress, nil
}

func (d *driver) endpoint() string {
	return d.config.GetString("vmax.endpoint")
}

func (d *driver) userName() string {
	return d.config.GetString("vmax.userName")
}

func (d *driver) password() string {
	return d.config.GetString("vmax.password")
}

func (d *driver) systemID() string {
	return d.config.GetString("vmax.systemID")
}

func (d *driver) thinOrThick() string {
	thinOrThick := d.config.GetString("vmax.thinOrThick")
	if thinOrThick == "" {
		return "ThinProvisioned"
	}
	return thinOrThick
}
