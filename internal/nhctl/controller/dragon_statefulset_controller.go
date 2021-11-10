package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	_const "nocalhost/internal/nhctl/const"
	"nocalhost/internal/nhctl/model"
	"nocalhost/internal/nhctl/profile"
	"nocalhost/internal/nhctl/utils"
	"nocalhost/pkg/nhctl/log"
	"strings"
	"time"
)

type DragonStatefulSetController struct {
	*Controller
}

func (ds *DragonStatefulSetController) GetNocalhostDevContainerPod() (string, error) {
	checkPodsList, err := ds.Client.ListPodByDragonStatefulset(ds.Name())
	if err != nil {
		return "", err
	}

	return findDevPod(checkPodsList.Items)
}

func (ds *DragonStatefulSetController) Name() string {
	return ds.Controller.Name
}

func (ds *DragonStatefulSetController) ReplaceImage(ctx context.Context, ops *model.DevStartOptions) error {
	var err error
	ds.Client.Context(ctx)

	dep, err := ds.Client.GetDragonStatefulSet(ds.Name())
	if err != nil {
		return err
	}
	originalSpecJson, err := json.Marshal(dep.Object["spec"])
	if err != nil {
		return errors.Wrap(err, "")
	}

	//if err = s.ScaleReplicasToOne(ctx); err != nil {
	//	return err
	//}
	ds.Client.Context(ctx)
	if err = ds.Client.ScaleDragonStatefulSetReplicasToOne(ds.Name()); err != nil {
		return err
	}

	devContainer, err := ds.Container(ops.Container)
	if err != nil {
		return err
	}

	devModeVolumes := make([]corev1.Volume, 0)
	devModeMounts := make([]corev1.VolumeMount, 0)

	// Set volumes
	syncthingVolumes, syncthingVolumeMounts := ds.generateSyncVolumesAndMounts()
	devModeVolumes = append(devModeVolumes, syncthingVolumes...)
	devModeMounts = append(devModeMounts, syncthingVolumeMounts...)

	workDirAndPersistVolumes, workDirAndPersistVolumeMounts, err := ds.genWorkDirAndPVAndMounts(
		ops.Container, ops.StorageClass,
	)
	if err != nil {
		return err
	}

	devModeVolumes = append(devModeVolumes, workDirAndPersistVolumes...)
	devModeMounts = append(devModeMounts, workDirAndPersistVolumeMounts...)

	workDir := ds.GetWorkDir(ops.Container)
	devImage := ds.GetDevImage(ops.Container) // Default : replace the first container
	if devImage == "" {
		return errors.New("Dev image must be specified")
	}

	sideCarContainer := generateSideCarContainer(ds.GetDevSidecarImage(ops.Container), workDir)

	devContainer.Image = devImage
	devContainer.Name = "nocalhost-dev"
	devContainer.Command = []string{"/bin/sh", "-c", "tail -f /dev/null"}
	devContainer.WorkingDir = workDir

	// set image pull policy
	sideCarContainer.ImagePullPolicy = _const.DefaultSidecarImagePullPolicy
	devContainer.ImagePullPolicy = _const.DefaultSidecarImagePullPolicy

	// add env
	devEnv := ds.GetDevContainerEnv(ops.Container)
	for _, v := range devEnv.DevEnv {
		env := corev1.EnvVar{Name: v.Name, Value: v.Value}
		devContainer.Env = append(devContainer.Env, env)
	}

	// Add volumeMounts to containers
	devContainer.VolumeMounts = append(devContainer.VolumeMounts, devModeMounts...)
	sideCarContainer.VolumeMounts = append(sideCarContainer.VolumeMounts, devModeMounts...)

	requirements := ds.genResourceReq(ops.Container)
	if requirements != nil {
		devContainer.Resources = *requirements
	}
	r := &profile.ResourceQuota{
		Limits:   &profile.QuotaList{Memory: "1Gi", Cpu: "1"},
		Requests: &profile.QuotaList{Memory: "50Mi", Cpu: "100m"},
	}
	rq, _ := convertResourceQuota(r)
	sideCarContainer.Resources = *rq

	needToRemovePriorityClass := false
	for i := 0; i < 10; i++ {
		events, err := ds.Client.ListEventsByStatefulSet(ds.Name())
		utils.Should(err)
		_ = ds.Client.DeleteEvents(events, true)

		// Get the latest stateful set
		dep, err = ds.Client.GetDragonStatefulSet(ds.Name())
		if err != nil {
			return err
		}
		podTemplateMap := dep.Object["spec"].(map[string]interface{})["template"]
		var podTemplate = &corev1.PodTemplateSpec{}
		mapstructure.Decode(podTemplateMap, podTemplate)
		if ops.Container != "" {
			for index, c := range podTemplate.Spec.Containers {
				if c.Name == ops.Container {
					podTemplate.Spec.Containers[index] = *devContainer
					break
				}
			}
		} else {
			podTemplate.Spec.Containers[0] = *devContainer
		}

		// Add volumes to deployment spec
		if podTemplate.Spec.Volumes == nil {
			log.Debugf("Service %s has no volume", dep.GetName())
			podTemplate.Spec.Volumes = make([]corev1.Volume, 0)
		}
		podTemplate.Spec.Volumes = append(podTemplate.Spec.Volumes, devModeVolumes...)

		// delete user's SecurityContext
		podTemplate.Spec.SecurityContext = &corev1.PodSecurityContext{}

		// disable readiness probes
		for i := 0; i < len(podTemplate.Spec.Containers); i++ {
			podTemplate.Spec.Containers[i].LivenessProbe = nil
			podTemplate.Spec.Containers[i].ReadinessProbe = nil
			podTemplate.Spec.Containers[i].StartupProbe = nil
			podTemplate.Spec.Containers[i].SecurityContext = nil
		}

		podTemplate.Spec.Containers = append(podTemplate.Spec.Containers, sideCarContainer)

		if dep.Object["metadata"].(map[string]interface{})["annotations"] == nil {
			dep.Object["metadata"].(map[string]interface{})["annotations"] = make(map[string]string, 0)
		}
		dep.Object["metadata"].(map[string]interface{})["annotations"] = string(originalSpecJson)

		log.Info("Updating development container...")
		mapstructure.Decode(podTemplate, podTemplateMap)
		dep.Object["spec"].(map[string]interface{})["template"] = podTemplateMap
		_, err = ds.Client.UpdateDragonStatefulset(dep, true)
		if err != nil {
			if strings.Contains(err.Error(), "Operation cannot be fulfilled on") {
				log.Warn("StatefulSet has been modified, retrying...")
				continue
			}
			return err
		} else {
			// Check if priorityClass exists
		outer:
			for i := 0; i < 20; i++ {
				time.Sleep(1 * time.Second)
				events, err = ds.Client.ListEventsByStatefulSet(ds.Name())
				for _, event := range events {
					if strings.Contains(event.Message, "no PriorityClass") {
						log.Warn("PriorityClass not found, disable it...")
						needToRemovePriorityClass = true
						break outer
					} else if event.Reason == "SuccessfulCreate" {
						log.Infof("Pod SuccessfulCreate")
						break outer
					}
				}
			}

			if needToRemovePriorityClass {
				dep, err = ds.Client.GetDragonStatefulSet(ds.Name())
				if err != nil {
					return err
				}
				podTemplateMap := dep.Object["spec"].(map[string]interface{})["template"]
				var podTemplate = &corev1.PodTemplateSpec{}
				mapstructure.Decode(podTemplateMap, podTemplate)
				podTemplate.Spec.PriorityClassName = ""
				log.Info("Removing priorityClass")
				mapstructure.Decode(podTemplate, podTemplateMap)
				dep.Object["spec"].(map[string]interface{})["template"] = podTemplateMap
				_, err = ds.Client.UpdateDragonStatefulset(dep, true)
				if err != nil {
					if strings.Contains(err.Error(), "Operation cannot be fulfilled on") {
						log.Warn("StatefulSet has been modified, retrying...")
						continue
					}
					return err
				}
				break
			}
		}
		break
	}
	return waitingPodToBeReady(ds.GetNocalhostDevContainerPod)
}

func (ds *DragonStatefulSetController) Container(containerName string) (*corev1.Container, error) {
	var devContainer *corev1.Container

	dss, err := ds.Client.GetDragonStatefulSet(ds.Name())
	if err != nil {
		return nil, err
	}
	podTemplateMap := dss.Object["spec"].(map[string]interface{})["template"]
	var podTemplate = &corev1.PodTemplateSpec{}
	mapstructure.Decode(podTemplateMap, podTemplate)
	if containerName != "" {
		for index, c := range podTemplate.Spec.Containers {
			if c.Name == containerName {
				return &podTemplate.Spec.Containers[index], nil
			}
		}
		return nil, errors.New(fmt.Sprintf("Container %s not found", containerName))
	} else {
		if len(podTemplate.Spec.Containers) > 1 {
			return nil, errors.New(
				fmt.Sprintf(
					"There are more than one container defined, " +
						"please specify one to start developing",
				),
			)
		}
		if len(podTemplate.Spec.Containers) == 0 {
			return nil, errors.New("No container defined ???")
		}
		devContainer = &podTemplate.Spec.Containers[0]
	}
	return devContainer, nil
}

func (ds *DragonStatefulSetController) RollBack(reset bool) error {
	return nil
}

func (s *DragonStatefulSetController) GetPodList() ([]corev1.Pod, error) {
	list, err := s.Client.ListPodByDragonStatefulset(s.Name())
	if err != nil {
		return nil, err
	}
	if list == nil || len(list.Items) == 0 {
		return nil, errors.New("no pod found")
	}
	return list.Items, nil
}