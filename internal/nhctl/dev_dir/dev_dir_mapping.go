package dev_dir

import (
	"fmt"
	"github.com/pkg/errors"
	"nocalhost/internal/nhctl/fp"
	"nocalhost/pkg/nhctl/log"
	"strings"
)

var NO_DEFAULT_PACK = errors.New("Current Svc pack not found ")

// get associate path of svcPack
// if no path match, try with svc with none container
func (svcPack *SvcPack) GetAssociatePath() DevPath {
	if !svcPack.valid() {
		log.Logf("Current svc is invalid to get associate path, %v", svcPack)
		return ""
	}

	var path DevPath
	if err := Get(
		func(dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) error {
			if _, ok := dirMapping.PackToPath[svcPack.Key()]; ok {
				path = dirMapping.PackToPath[svcPack.Key()]
			} else {
				path = dirMapping.PackToPath[svcPack.keyWithoutContainer()]
			}
			return nil
		},
	); err != nil {
		log.ErrorE(err, fmt.Sprintf("Current svc is fail to get associate path, %v", svcPack))
		return ""
	}
	return path
}

// return "" if error occur
func (svcPack *SvcPack) GetKubeConfigBytes() string {
	if !svcPack.valid() {
		log.Logf("Current svc is invalid to get associate path, %v", svcPack)
		return ""
	}

	var kubeconfigContent string
	if err := Get(
		func(dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) error {
			if _, ok := dirMapping.PackToPath[svcPack.Key()]; ok {
				kubeconfigContent = dirMapping.PackToKubeConfigBytes[svcPack.Key()]
			} else {
				kubeconfigContent = dirMapping.PackToKubeConfigBytes[svcPack.keyWithoutContainer()]
			}
			return nil
		},
	); err != nil {
		log.ErrorE(err, fmt.Sprintf("Current svc is fail to get associate path, %v", svcPack))
		return ""
	}
	return kubeconfigContent
}

func (svcPack *SvcPack) UnAssociatePath() {
	if !svcPack.valid() {
		log.Logf("Current svc is invalid to get associate path, %v", svcPack)
	}

	if err := Update(
		func(dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) error {
			delete(dirMapping.PackToPath, svcPack.Key())
			delete(dirMapping.PackToPath, svcPack.keyWithoutContainer())
			return nil
		},
	); err != nil {
		log.ErrorE(err, fmt.Sprintf("Current svc is fail to get associate path, %v", svcPack))
	}
}

// return error if not found
func (d DevPath) GetDefaultPack() (*SvcPack, error) {
	return getDefaultPack(d)
}

func (d DevPath) GetAllPacks() *AllSvcPackAssociateByPath {
	return getAllPacks(d)
}

// Associate setAsDefaultSvc:
// if this dev path has been associate by svc && [setAsDefaultSvc==true]
// replace the default svc to the path
//
// setAsDefaultSvc==false when data migration
func (d DevPath) Associate(specifyPack *SvcPack, kubeconfig string, setAsDefaultSvc bool) error {
	if !specifyPack.valid() {
		return errors.New("Svc pack is invalid")
	}

	// step.1 remove all mapping from specify pack
	// step.2 build mapping from specifyPack to current path and associate kubeconfig and pack
	// step.3 mark specifyPack as default pack to current path

	return d.removePackAndThen(
		specifyPack,
		func(dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) error {
			kubeconfigContent := fp.NewFilePath(kubeconfig).ReadFile()
			if kubeconfigContent == "" {
				log.Log("Associate Svc %s but kubeconfig is nil", specifyPack.Key())
				return nil
			}

			key := specifyPack.Key()
			keyWithoutContainer := specifyPack.keyWithoutContainer()

			dirMapping.PackToPath[key] = d
			dirMapping.PackToKubeConfigBytes[key] = kubeconfigContent

			// if container not specified
			// cover all pack with same ns/app/type/svc
			if specifyPack.Container == "" {
				for keyItem, _ := range dirMapping.PackToPath {
					if strings.HasPrefix(string(keyItem), string(key)) {
						dirMapping.PackToPath[keyItem] = d
						dirMapping.PackToKubeConfigBytes[keyItem] = kubeconfigContent
					}
				}
			} else {
				dirMapping.PackToPath[keyWithoutContainer] = d
				dirMapping.PackToKubeConfigBytes[keyWithoutContainer] = kubeconfigContent
			}

			if _, hasBeenSet := dirMapping.PathToDefaultPackKey[d]; setAsDefaultSvc || !hasBeenSet {
				dirMapping.PathToDefaultPackKey[d] = specifyPack.Key()
			}

			return nil
		},
	)
}

func (d DevPath) RemovePack(specifyPack *SvcPack) error {
	return d.removePackAndThen(specifyPack, nil)
}

func (d DevPath) removePackAndThen(
	specifyPack *SvcPack,
	fun func(dirMapping *DevDirMapping,
	pathToPack map[DevPath][]*SvcPack) error) error {
	if !specifyPack.valid() {
		return errors.New("Svc pack is invalid")
	}

	return Update(
		func(dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) error {
			specifyPackKey := specifyPack.Key()
			devPathBefore := dirMapping.PackToPath[specifyPack.Key()]

			beforePacks := doGetAllPacks(devPathBefore, dirMapping, pathToPack)

			// step.1 remove or modify the before path's default packKey
			// step.2 remove mapping of specify pack to current path
			// stop.3 call fun

			// 1 -
			// if specify Svc has been associate with before path and if it is a default
			// pack of a path, should modify or remove the default Svc pack of the path
			//
			{
				// remove [path -> defaultSvc] directly if len==1
				if len(beforePacks.Packs) == 1 {
					delete(dirMapping.PathToDefaultPackKey, d)

					// modify [path -> defaultSvc] if defaultSvc == specifyPackKey
				} else if beforePacks.DefaultSvcPackKey == specifyPackKey {

					// modify the before path's default packKey to a random packKey
					for random, _ := range beforePacks.Packs {
						if random != specifyPackKey {
							dirMapping.PathToDefaultPackKey[devPathBefore] = random
						}
					}
				} else {
					// do not need to remove default pack key
				}
			}

			// 2 -
			delete(dirMapping.PackToPath, specifyPack.Key())

			// 3 -
			if fun == nil {
				return nil
			} else {
				return fun(dirMapping, pathToPack)
			}
		},
	)
}

func (svcPack *SvcPack) valid() bool {
	return svcPack != nil &&
		svcPack.Ns != "" && svcPack.App != "" &&
		svcPack.SvcType != "" && svcPack.Svc != ""
}

func getDefaultPack(path DevPath) (*SvcPack, error) {
	packs := getAllPacks(path)
	defaultSvcPackKey := packs.DefaultSvcPackKey

	if pack, ok := packs.Packs[defaultSvcPackKey]; ok {
		return pack, nil
	}

	return nil, NO_DEFAULT_PACK
}

// list all pack associate with this path
// this method will not access the db
func doGetAllPacks(path DevPath, dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) *AllSvcPackAssociateByPath {
	var r *AllSvcPackAssociateByPath

	packs, ok := pathToPack[path]

	defaultSvcPackKey := dirMapping.PathToDefaultPackKey[path]

	allpacks := make(map[SvcPackKey]*SvcPack, 0)
	KubeConfigs := make(map[SvcPackKey]string, 0)
	if ok {
		for _, pack := range packs {
			packKey := pack.Key()
			allpacks[packKey] = pack
			KubeConfigs[packKey] = dirMapping.PackToKubeConfigBytes[packKey]
		}
	}

	r = &AllSvcPackAssociateByPath{
		Packs:             allpacks,
		DefaultSvcPackKey: defaultSvcPackKey,
		Kubeconfigs:       KubeConfigs,
	}
	return r
}

// list all pack associate with this path
func getAllPacks(path DevPath) *AllSvcPackAssociateByPath {
	var r *AllSvcPackAssociateByPath
	if err := Get(
		func(dirMapping *DevDirMapping, pathToPack map[DevPath][]*SvcPack) error {
			r = doGetAllPacks(path, dirMapping, pathToPack)
			return nil
		},
	); err != nil {
		return nil
	}
	return r
}
