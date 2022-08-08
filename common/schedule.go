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

	"github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type evictionVersion string

const none evictionVersion = "none"
const v1beta1 evictionVersion = "v1beta1"
const v1 evictionVersion = "v1"

func fetchEvictionVersion(ki kubernetes.Interface) (evictionVersion, error) {
	serverVersion, err := GetAPIServerVersion(ki)
	if err != nil {
		return none, err
	}
	if serverVersion.GE(semver.MustParse("1.22.0")) {
		return v1, nil
	}
	if serverVersion.GE(semver.MustParse("1.6.0")) {
		return v1beta1, nil
	}
	return none, err
}

// Checks if the is Schedulable according to schedulable and patches the node if necessary.
func EnsureSchedulable(ctx context.Context, k8sClient client.Client, node *corev1.Node, schedulable bool) error {
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

type DrainParameters struct {
	AwaitDeletion WaitParameters
	Eviction      WaitParameters
	Client        client.Client
	// for eviction API as that is not callable from client.Client
	Clientset kubernetes.Interface
	// when set to true and eviction creation fails
	// within the eviction timeout, call a direct delete
	// on pods afterwards.
	ForceEviction      bool
	GracePeriodSeconds *int64
}

// Drains Pods from the given node, if required.
func EnsureDrain(ctx context.Context, node *corev1.Node, log logr.Logger, params DrainParameters) error {
	deletable, err := GetPodsForDrain(ctx, params.Client, node.Name)
	if err != nil {
		return fmt.Errorf("failed to fetch deletable pods: %w", err)
	}
	if len(deletable) == 0 {
		return nil
	}
	version, err := fetchEvictionVersion(params.Clientset)
	if err != nil {
		return err
	}
	if version == none {
		log.Info("Going to delete pods from node.", "count", len(deletable), "node", node.Name)
		err = deletePods(ctx, params.Client, deletable, params.GracePeriodSeconds)
	} else {
		log.Info("Going to evict pods from node.", "count", len(deletable), "node", node.Name)
		err = evictPods(ctx, params.Clientset, deletable, version, params.Eviction, params.GracePeriodSeconds)
		if err != nil && params.ForceEviction {
			log.Info("Eviction failed, going to delete pods", "err", err)
			err = deletePods(ctx, params.Client, deletable, params.GracePeriodSeconds)
		}
	}
	if err != nil {
		return fmt.Errorf("failed to delete/evict at least one pod: %w", err)
	}
	log.Info("Awaiting pod deletion.", "period", params.AwaitDeletion.Period,
		"timeout", params.AwaitDeletion.Timeout)
	err = WaitForPodDeletions(ctx, params.Client, deletable, params.AwaitDeletion)
	if err != nil {
		return fmt.Errorf("failed to await pod deletions: %w", err)
	}
	return nil
}

// Gets a list of pods to be deleted for a node to be considered drained.
// Shortened https://github.com/kinvolk/flatcar-linux-update-operator/blob/master/pkg/k8sutil/drain.go .
func GetPodsForDrain(ctx context.Context, k8sClient client.Client, nodeName string) ([]corev1.Pod, error) {
	var podList corev1.PodList
	err := k8sClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": nodeName})
	if err != nil {
		return nil, err
	}
	// filter
	filtered := make([]corev1.Pod, 0)
	for _, pod := range podList.Items {
		// skip mirror pods
		if _, ok := pod.Annotations[corev1.MirrorPodAnnotationKey]; ok {
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

func deletePods(ctx context.Context, k8sClient client.Client, pods []corev1.Pod, gracePeriodSeconds *int64) error {
	var sumErr error
	for i := range pods {
		pod := pods[i]
		err := k8sClient.Delete(ctx, &pod, &client.DeleteOptions{GracePeriodSeconds: gracePeriodSeconds})
		if err != nil && !errors.IsNotFound(err) {
			sumErr = fmt.Errorf("failed to delete pod %s from node: %w", pod.Name, sumErr)
		}
	}
	return sumErr
}

func evictPods(ctx context.Context, ki kubernetes.Interface, pods []corev1.Pod,
	version evictionVersion, params WaitParameters, gracePeriodSeconds *int64) error {
	if len(pods) == 0 {
		return nil
	}
	waiters := make([]WaitFunc, 0)
	for _, pod := range pods {
		waiters = append(waiters, func() error {
			return evictPod(ctx, ki, pod, version, params, gracePeriodSeconds)
		})
	}
	return waitParallel(waiters)
}

type WaitParameters struct {
	Period  time.Duration
	Timeout time.Duration
}

func evictPod(ctx context.Context, ki kubernetes.Interface, pod corev1.Pod,
	version evictionVersion, params WaitParameters, gracePeriodSeconds *int64) error {
	return wait.PollImmediateWithContext(ctx, params.Period, params.Timeout, func(ctx context.Context) (bool, error) {
		var err error
		if version == v1beta1 {
			eviction := policyv1beta1.Eviction{}
			eviction.Name = pod.Name
			eviction.Namespace = pod.Namespace
			eviction.DeletionGracePeriodSeconds = gracePeriodSeconds
			err = ki.CoreV1().Pods(pod.Namespace).EvictV1beta1(ctx, &eviction)
		}
		if version == v1 {
			eviction := policyv1.Eviction{}
			eviction.Name = pod.Name
			eviction.Namespace = pod.Namespace
			eviction.DeletionGracePeriodSeconds = gracePeriodSeconds
			err = ki.CoreV1().Pods(pod.Namespace).EvictV1(ctx, &eviction)
		}
		if err != nil {
			return false, err
		}
		return true, nil
	})
}

// Deletes the given pods and awaits there deletion.
func WaitForPodDeletions(ctx context.Context, client client.Client, pods []corev1.Pod, params WaitParameters) error {
	if len(pods) == 0 {
		return nil
	}
	waiters := make([]WaitFunc, 0)
	for _, pod := range pods {
		waiters = append(waiters, func() error {
			return waitForPodDeletion(ctx, client, pod, params)
		})
	}
	return waitParallel(waiters)
}

type WaitFunc = func() error

func waitParallel(waiters []WaitFunc) error {
	errChan := make(chan error)
	for _, waiter := range waiters {
		go func(waiter func() error) {
			errChan <- waiter()
		}(waiter)
	}
	combinedMessage := ""
	count := 0
	for err := range errChan {
		if err != nil {
			combinedMessage += fmt.Sprintf("%s + ", err)
		}
		count++
		if count == len(waiters) {
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
func waitForPodDeletion(ctx context.Context, client client.Client, pod corev1.Pod, params WaitParameters) error {
	return wait.PollImmediate(params.Period, params.Timeout, func() (bool, error) {
		var p corev1.Pod
		err := client.Get(ctx, types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}, &p)
		if errors.IsNotFound(err) || (p.ObjectMeta.UID != pod.ObjectMeta.UID) {
			return true, nil
		} else if err != nil {
			return false, err
		}
		return false, nil
	})
}
