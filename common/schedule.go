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

package common

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Checks if the is Schedulable according to schedulable and patches the node if necessary.
func EnsureSchedulable(ctx context.Context, k8sClient client.Client, node *v1.Node, schedulable bool) error {
	// If node already has the correct value
	if node.Spec.Unschedulable != schedulable {
		return nil
	}
	cloned := node.DeepCopy()
	node.Spec.Unschedulable = !schedulable
	err := k8sClient.Patch(ctx, node, client.MergeFrom(cloned))
	if err != nil {
		return fmt.Errorf("Failed to set node %v as (un-)schedulable: %w", node.Name, err)
	}
	return nil
}

// Drains Pods from the given node, if required.
func EnsureDrain(ctx context.Context, node *v1.Node, log logr.Logger, params WaitParameters) error {
	deletable, err := GetPodsForDrain(ctx, params.Client, node.Name)
	if err != nil {
		return fmt.Errorf("failed to fetch deletable pods: %w", err)
	}
	if len(deletable) == 0 {
		return nil
	}
	log.Info("Going to delete pods from node.", "count", len(deletable), "node", node.Name)
	deleteFailed := false
	for i := range deletable {
		pod := deletable[i]
		err = params.Client.Delete(ctx, &pod, &client.DeleteOptions{})
		if err != nil {
			log.Error(err, "Failed to delete pod from node.", "node", node.Name, "pod", pod.Name)
			deleteFailed = true
		}
	}
	if deleteFailed {
		return fmt.Errorf("failed to delete at least one pod")
	}
	log.Info("Awaiting pod deletion.", "period", params.Period,
		"timeout", params.Timeout)
	err = WaitForPodDeletions(ctx, deletable, params)
	if err != nil {
		return fmt.Errorf("failed to await pod deletions: %w", err)
	}
	return nil
}

// Gets a list of pods to be deleted for a node to be considered drained.
// Shortened https://github.com/kinvolk/flatcar-linux-update-operator/blob/master/pkg/k8sutil/drain.go .
func GetPodsForDrain(ctx context.Context, k8sClient client.Client, nodeName string) ([]v1.Pod, error) {
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

// Deletes the given pods and awaits there deletion.
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
