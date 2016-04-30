package vsphere

import (
	"github.com/Sirupsen/logrus"
	"github.com/emc-advanced-dev/unik/pkg/providers/common"
	"github.com/emc-advanced-dev/unik/pkg/types"
	"github.com/layer-x/layerx-commons/lxerrors"
	"os"
	"path/filepath"
	"time"
	"strings"
	"io/ioutil"
	unikutil "github.com/emc-advanced-dev/unik/pkg/util"
)

func (p *VsphereProvider) Stage(name string, rawImage *types.RawImage, force bool) (_ *types.Image, err error) {
	images, err := p.ListImages()
	if err != nil {
		return nil, lxerrors.New("retrieving image list for existing image", err)
	}
	for _, image := range images {
		if image.Name == name {
			if !force {
				return nil, lxerrors.New("an image already exists with name '"+name+"', try again with --force", nil)
			} else {
				logrus.WithField("image", image).Warnf("force: deleting previous image with name " + name)
				err = p.DeleteImage(image.Id, true)
				if err != nil {
					return nil, lxerrors.New("removing previously existing image", err)
				}
			}
		}
	}
	c := p.getClient()
	vsphereImageDir := getImageDatastoreDir(name)
	if err := c.Mkdir(vsphereImageDir); err != nil && !strings.Contains(err.Error(), "exists") {
		return nil, lxerrors.New("creating vsphere directory for image", err)
	}
	defer func() {
		if err != nil {
			logrus.WithError(err).Warnf("creating image failed, cleaning up image on datastore")
			c.Rmdir(vsphereImageDir)
		}
	}()

	localVmdkDir, err := ioutil.TempDir(unikutil.UnikTmpDir(), "")
	if err != nil {
		return nil, lxerrors.New("creating tmp file", err)
	}
	defer os.RemoveAll(localVmdkDir)
	localVmdkFile := filepath.Join(localVmdkDir, "boot.vmdk")

	logrus.WithField("raw-image", rawImage).Infof("creating boot volume from raw image")
	if err := common.ConvertRawImage("vmdk", rawImage.LocalImagePath, localVmdkFile); err != nil {
		return nil, lxerrors.New("converting raw image to vmdk", err)
	}

	rawImageFile, err := os.Stat(localVmdkFile)
	if err != nil {
		return nil, lxerrors.New("statting raw image file", err)
	}
	sizeMb := rawImageFile.Size() >> 20

	logrus.WithFields(logrus.Fields{
		"name": name,
		"id":   name,
		"size": sizeMb,
		"datastore-path": vsphereImageDir,
	}).Infof("importing base vmdk for unikernel image")

	if err := c.ImportVmdk(localVmdkFile, vsphereImageDir); err != nil {
		return nil, lxerrors.New("importing base boot.vmdk to vsphere datastore", err)
	}

	image := &types.Image{
		Id:             name,
		Name:           name,
		DeviceMappings: rawImage.DeviceMappings,
		SizeMb:         sizeMb,
		Infrastructure: types.Infrastructure_VSPHERE,
		Created:        time.Now(),
	}

	err = p.state.ModifyImages(func(images map[string]*types.Image) error {
		images[name] = image
		return nil
	})
	if err != nil {
		return nil, lxerrors.New("modifying image map in state", err)
	}
	err = p.state.Save()
	if err != nil {
		return nil, lxerrors.New("saving image map to state", err)
	}

	logrus.WithFields(logrus.Fields{"image": image}).Infof("image created succesfully")
	return image, nil
}
