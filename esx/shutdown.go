/*******************************************************************************
*
* Copyright 2020 SAP SE
*
* Licensed under the Apache License, Version 2.0 (the "License");
* you may not use this file except in compliance with the License.
* You should have received a copy of the License along with this
* program. If not, you may obtain a copy of the License at
*
*     http://www.apache.org/licenses/LICENSE-2.0
*
* Unless required by applicable law or agreed to in writing, software
* distributed under the License is distributed on an "AS IS" BASIS,
* WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
* See the License for the specific language governing permissions and
* limitations under the License.
*
*******************************************************************************/

package esx

import (
	"context"
	"fmt"
	"time"

	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/property"
	"github.com/vmware/govmomi/view"
	"github.com/vmware/govmomi/vim25/mo"
	vctypes "github.com/vmware/govmomi/vim25/types"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Checks, if all Nodes on an ESX need maintenance and are allowed to be shutdown.
// If so the RebootInitated Annotation is set on the affected Nodes.
func ShouldShutdown(esx *Host) bool {
	var initCount int
	for _, node := range esx.Nodes {
		if node.Labels[MaintenanceLabelKey] == string(InMaintenance) && node.Labels[RebootOkLabelKey] == TrueString {
			initCount++
		}
	}
	return initCount == len(esx.Nodes)
}

// Shortened https://github.com/kinvolk/flatcar-linux-update-operator/blob/master/pkg/k8sutil/drain.go
func GetPodsForDeletion(ctx context.Context, k8sClient client.Client, nodeName string) ([]v1.Pod, error) {
	var podList v1.PodList
	err := k8sClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": nodeName})
	if err != nil {
		return nil, err
	}
	// filter
	filtered := make([]v1.Pod, 0)
	for _, pod := range podList.Items {
		// skip mirror pods
		if _, ok := pod.Annotations[v1.MirrorPodAnnotationKey]; ok {
			continue
		}
		// skip daemonsets
		skip := false
		for _, ref := range pod.OwnerReferences {
			if ref.Kind == "DaemonSet" {
				skip = true
			}
		}
		if skip {
			continue
		}
		filtered = append(filtered, pod)
	}
	return filtered, nil
}

type WaitParameters struct {
	Client  client.Client
	Period  time.Duration
	Timeout time.Duration
}

func WaitForPodDeletions(ctx context.Context, pods []v1.Pod, params WaitParameters) error {
	if len(pods) == 0 {
		return nil
	}
	errChan := make(chan error)
	for _, pod := range pods {
		go func(pod v1.Pod) {
			errChan <- waitForPodDeletion(ctx, pod, params)
		}(pod)
	}
	combinedMessage := ""
	count := 0
	for err := range errChan {
		if err != nil {
			combinedMessage += fmt.Sprintf("%s + ", err)
		}
		count++
		if count == len(pods) {
			close(errChan)
		}
	}
	if combinedMessage == "" {
		return nil
	}
	combinedMessage = combinedMessage[:len(combinedMessage)-3]
	return fmt.Errorf("%s", combinedMessage)
}

// Shortened https://github.com/kinvolk/flatcar-linux-update-operator/blob/master/pkg/agent/agent.go#L470
func waitForPodDeletion(ctx context.Context, pod v1.Pod, params WaitParameters) error {
	return wait.PollImmediate(params.Period, params.Timeout, func() (bool, error) {
		var p v1.Pod
		err := params.Client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &p)
		if errors.IsNotFound(err) || (p.ObjectMeta.UID != pod.ObjectMeta.UID) {
			return true, nil
		} else if err != nil {
			return false, err
		}
		return false, nil
	})
}

func ensureVmOff(ctx context.Context, vCenters *VCenters, info HostInfo, nodeName string) error {
	client, err := vCenters.Client(ctx, info.AvailabilityZone)
	if err != nil {
		return fmt.Errorf("Failed to connect to vCenter: %w", err)
	}
	mgr := view.NewManager(client.Client)
	view, err := mgr.CreateContainerView(ctx, client.ServiceContent.RootFolder,
		[]string{"VirtualMachine"}, true)
	if err != nil {
		return fmt.Errorf("Failed to create container view: %w", err)
	}
	var vms []mo.VirtualMachine
	err = view.RetrieveWithFilter(ctx, []string{"VirtualMachine"}, []string{"summary.runtime"},
		&vms, property.Filter{"name": nodeName})
	if err != nil {
		return fmt.Errorf("Failed to retrieve VM %v", nodeName)
	}
	if len(vms) != 1 {
		return fmt.Errorf("Expected to retrieve 1 VM from vCenter, but got %v", len(vms))
	}
	if vms[0].Summary.Runtime.PowerState == vctypes.VirtualMachinePowerStatePoweredOff {
		return nil
	}
	vm := object.NewVirtualMachine(client.Client, vms[0].Self)
	task, err := vm.PowerOff(ctx)
	if err != nil {
		return fmt.Errorf("Failed to create poweroff task for VM %v", nodeName)
	}
	taskResult, err := task.WaitForResult(ctx)
	if err != nil {
		return fmt.Errorf("Failed to await poweroff task for VM %v", nodeName)
	}
	if taskResult.State != vctypes.TaskInfoStateSuccess {
		return fmt.Errorf("VM %v poweroff task was not successful", nodeName)
	}
	return nil
}
