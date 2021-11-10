package clientgoutils

import (
	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"nocalhost/pkg/nhctl/log"
)

func (c *ClientGoUtils) UpdateDragonStatefulset(obj *unstructured.Unstructured, wait bool) (*unstructured.Unstructured, error) {
	dss, err := c.dynamicClient.Resource(dsts).Update(c.ctx, obj, metav1.UpdateOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "")
	}
	if !wait {
		return dss, nil
	}
	if dss.UnstructuredContent()["status"].(map[string]interface{})["ready"] == 1 {
		return dss, nil
	}
	log.Debug("StatefulSet has not been ready yet")
	if err = c.WaitForCustomResourceReady(dsts, dss.GetName(), isDragonStatefulSetReady); err != nil {
		return nil, err
	}
	return dss, nil
}

func (c *ClientGoUtils) ScaleDragonStatefulSetReplicasToOne(name string) error {
	dss, err := c.dynamicClient.Resource(dsts).Get(c.ctx, name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	currentReplicas := dss.Object["spec"].(map[string]interface{})["replicas"].(int32)
	if currentReplicas > 1 || currentReplicas == 0 {
		dss.Object["spec"].(map[string]interface{})["replicas"] = 1
		_, err := c.dynamicClient.Resource(dsts).Update(c.ctx, dss, metav1.UpdateOptions{})
		return err
	} else {
		log.Info("Replicas has already been scaled to 1")
	}
	return nil
}