package storage

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	log "github.com/Sirupsen/logrus"
	"github.com/akutz/gofig"
	"github.com/akutz/goof"

	symm "github.com/emccode/govmax/api/v2"
	symmtypes "github.com/emccode/govmax/api/v2/model"

	"github.com/emccode/libstorage/api/context"
	"github.com/emccode/libstorage/api/registry"
	"github.com/emccode/libstorage/api/types"
	"github.com/emccode/libstorage/drivers/storage/vmax"
)

type driver struct {
	config         gofig.Config
	client         symm.Client
	symmetrix      symmtypes.Symmetrix
	groupPrefixID string
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
	d.config = config
	d.client = symm.NewClient(
		d.endpoint(),
		d.userName(),
		d.password(),
		d.port(),
		d.insecure())

	symmetrix, err := d.client.GetSymmetrix(d.symmetrixID())
	if err != nil {
		return fmt.Errorf("Error Finding Symmetrix %s. %s", d.symmetrixID(), err)
	}

	d.symmetrix = symmetrix
	d.groupPrefixID = d.groupPrefixID()

	err = d.initStorageGroup()
	if err != nil {
		return fmt.Errorf("Error creating storage group for use with libStorage! %s", err)
	}

	fields := eff(map[string]interface{}{
		"symmetrixId": d.symmetrix.SymmetrixID,
		"endpoint":    d.endpoint(),
		"port":        d.port(),
		"username":    d.userName(),
		"password":    "******",
		"insecure":    d.insecure(),
	})

	log.WithFields(fields).Debug("starting vmax driver")
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

	var initiatorName string
	if err := iid.UnmarshalMetadata(&initiatorName); err != nil {
		return nil, err
	}

	hosts, err := d.client.ListHosts(d.symmetrix.SymmetrixID)
	if err != nil {
		return nil, err
	}

	for _, hostID := range hosts.HostID {
		host, err := d.client.GetHost(d.symmetrix.SymmetrixID, hostID)
		if err != nil {
			return nil, err
		}

		initiators := host.Host[0].Initiator
		for _, initiator := range initiators {
			if initiator == initiatorName {
				instance := types.Instance{
					InstanceID: &types.InstanceID{
						ID: initiator,
					},
				}
				return &instance, nil
			}
		}
	}

	return nil, fmt.Errorf("Host is not connected to VMAX")
}

func (d *driver) Volumes(
	ctx types.Context,
	opts *types.VolumesOpts) ([]*types.Volume, error) {
	symmetrixID := d.symmetrix.SymmetrixID

	vmaxVolumes, err := d.client.ListVolumes(symmetrixID, "", true, false)
	if err != nil {
		return []*types.Volume{}, err
	}

	volumesResult := vmaxVolumes.ResultList.Result
	volumes := make([]*types.Volume, len(volumesResult))

	for i, v := range volumesResult {
		volume := &types.Volume{
			ID: v.VolumeID,

			Status: "N/A - Query Volume Directly",
			Name:   "N/A - Query Volume Directly",
			Type:   "N/A - Query Volume Directly",
			Attachments: []*types.VolumeAttachment{
				&types.VolumeAttachment{
					InstanceID: &types.InstanceID{
						ID:     "N/A - Query Volume Directly",
						Driver: "VMAX",
					},
				},
			},
		}

		volumes[i] = volume
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

	vmaxVolume, err := d.client.GetVolume(d.symmetrix.SymmetrixID, volumeID)
	if err != nil {
		return nil, err
	}

	volume := &types.Volume{
		Name:   vmaxVolume.Volume[0].VolumeIdentifier,
		ID:     vmaxVolume.Volume[0].VolumeID,
		Status: "",
		Type:   vmaxVolume.Volume[0].Type,
		Size:   int64(vmaxVolume.Volume[0].CapGb),
	}

	log.Debug("Getting volume's attachments")
	attachments, err := d.client.GetAttachments(d.symmetrix.SymmetrixID, volumeID)
	if err != nil {
		return volume, err
	}

	log.Debug("Done getting volume's attachments")
	volume.Attachments = make([]*types.VolumeAttachment, len(attachments.InitiatorIDs))
	for i, initiatorID := range attachments.InitiatorIDs {
		volume.Attachments[i] = &types.VolumeAttachment{
			InstanceID: &types.InstanceID{
				ID: initiatorID,
			},
		}
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
	if opts.Size == nil {
		return &types.Volume{}, fmt.Errorf("Need to specify size in VolumeCreateOpts")
	}

	volIDs, err := d.client.CreateVolume(d.symmetrix.SymmetrixID, strconv.Itoa(int(*opts.Size)), "FBA", 1)
	if err != nil {
		return nil, fmt.Errorf("unable to create volume: %s", err)
	}

	volume := &types.Volume{
		ID:               volIDs[0],
		Size:             *opts.Size,
		Type:             *opts.Type,
		AvailabilityZone: *opts.AvailabilityZone,
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

	if err := d.client.DeleteVolume(d.symmetrix.SymmetrixID, volumeID); err != nil {
		return goof.WithFieldsE(fields, "error deleting volume", err)
	}

	log.WithFields(fields).Debug("removed volume")
	return nil
}

func (d *driver) VolumeAttach(
	ctx types.Context,
	volumeID string,
	opts *types.VolumeAttachOpts) (*types.Volume, string, error) {

	iid := context.MustInstanceID(ctx)
	re := regexp.MustCompile("[^\\w]")
	instanceIDCompiled := re.ReplaceAllString(iid.ID, "")
	storageGroup, err := d.client.GetStorageGroup(d.symmetrix.SymmetrixID, d.groupPrefixID+instanceIDCompiled)
	if strings.Contains(err.Error(), "error finding storagegroup") {
		err = d.client.CreateStorageGroup(d.symmetrix.SymmetrixID, "","", d.groupPrefixID+instanceIDCompiled)
		if err != nil {
			return nil, "", goof.WithError("error creating storagegroup. %s", err)
		}
	}
	host, err := d.client.GetHost(d.symmetrix.SymmetrixID,d.groupPrefixID+instanceIDCompiled)
	if strings.Contains(err.Error(), "error getting host") {
		
		err = d.client.CreateHost(p model.CreateHostParam)
	}







		if storageGroup.StorageGroup[0].NumOfMaskingViews > 0 {
			for _, maskingViewID := range storageGroup.StorageGroup[0].Maskingview {
				maskingView, err := d.client.GetMaskingView(d.symmetrix.SymmetrixID, maskingViewID)
				if err != nil {
					return nil, "", goof.WithError("error getting maskingview. %s", err)
				}
				if maskingView.MaskingView[0].HostID != "" {
					host, err := d.client.GetHost(d.symmetrix.SymmetrixID, maskingView.MaskingView[0].HostID)
					if err != nil {
						return nil, "", goof.WithError("error getting host. %s", err)
					}
					for _, initiator := range host.Host[0].Initiator {
						if initiator == iid.ID {
							if err := d.client.AddVolumeToStorageGroup(d.symmetrix.SymmetrixID, d.groupPrefixID, volumeID); err != nil {
								return nil, "", goof.WithError("error adding volume to storage group. %s", err)
							}

							attachedVol, err := d.VolumeInspect(
								ctx, volumeID, &types.VolumeInspectOpts{
									Attachments: true,
									Opts:        opts.Opts,
								})
							if err != nil {
								return nil, "", goof.WithError("error getting volume after adding to storage group. %s", err)
							}

							return attachedVol, attachedVol.ID, nil
						}
					}
				}
				//hostID := range maskingView.MaskingView[0].HostID
			}
		} else {
			//create maskingView
			//check for hostgroup
		}
	}
	// hosts, err := d.client.ListHosts(d.symmetrix.SymmetrixID)
	// if err != nil {
	// 	return nil, "", goof.WithError("error getting list of hosts. %s", err)
	// }
	//
	// for _, hostid := range hosts.HostID {
	// 	host, err := d.client.GetHost(d.symmetrix.SymmetrixID, hostid)
	// if err != nil {
	// 	return nil, "", goof.WithError("error getting host. %s", err)
	// }
	// 	for _, initiator := range host.Host[0].Initiatorid {
	// 		if initiator == iid.ID{
	// 			maskingview, err := d.client.GetMaskingView(d.symmetrix.SymmetrixID, host.Host[0].Maskingview)
	// 		}
	// 	}
	//
	// }

}

func (d *driver) VolumeDetach(
	ctx types.Context,
	volumeID string,
	opts *types.VolumeDetachOpts) (*types.Volume, error) {

	if err := d.client.RemoveVolumeFromStorageGroup(d.symmetrix.SymmetrixID, d.groupPrefixID, volumeID); err != nil {
		return nil, goof.WithError("error removing volume from storage group", err)
	}

	detachedVol, _ := d.VolumeInspect(
		ctx, volumeID, &types.VolumeInspectOpts{
			Attachments: true,
			Opts:        opts.Opts,
		})

	return detachedVol, nil
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
		"provider": "vmax",
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

func (d *driver) endpoint() string {
	return d.config.GetString("vmax.endpoint")
}

func (d *driver) userName() string {
	return d.config.GetString("vmax.userName")
}

func (d *driver) password() string {
	return d.config.GetString("vmax.password")
}

func (d *driver) symmetrixID() string {
	return d.config.GetString("vmax.symmetrixID")
}

func (d *driver) storageGroupID() string {
	return d.config.GetString("vmax.storageGroupID")
}

func (d *driver) insecure() bool {
	return d.config.GetBool("vmax.insecure")
}

func (d *driver) port() string {
	return d.config.GetString("vmax.port")
}

func (d *driver) thinOrThick() string {
	thinOrThick := d.config.GetString("vmax.thinOrThick")
	if thinOrThick == "" {
		return "ThinProvisioned"
	}
	return thinOrThick
}

func (d *driver) initStorageGroup() error {
	_, err := d.client.GetStorageGroup(d.symmetrix.SymmetrixID, d.groupPrefixID)
	if err != nil {
		if strings.Contains(err.Error(), "Cannot find Storage Group") {
			err = d.client.CreateStorageGroup(d.symmetrix.SymmetrixID, "", "", d.groupPrefixID)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}

	return nil
}
