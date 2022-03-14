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

package metrics

import (
	"context"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	shuffleCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "maintenance_controller_pod_shuffle_count",
		Help: "Counts pods in DaemonSets, Deployments and StatefulSets, " +
			"that were likely shuffled by a node send into maintenance",
	}, []string{"owner", "profile"})

	shufflesPerReplica = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "maintenance_controller_pod_shuffles_per_replica",
		Help: "Count of pods in DaemonSets, Deployments and StatefulSets, " +
			"that were likely shuffled by a node send into maintenance, " +
			"divided by the replica count when the event occurred",
	}, []string{"owner", "profile"})
)

func RegisterMaintenanceMetrics() {
	metrics.Registry.MustRegister(shuffleCount, shufflesPerReplica)
}

type shuffleRecord struct {
	owner      string
	perReplica float64
}

type fetchParams struct {
	ctx    context.Context
	client client.Client
	ref    types.NamespacedName
}

func RecordShuffles(ctx context.Context, k8sClient client.Client, node *v1.Node, currentProfile string) error {
	var podList v1.PodList
	err := k8sClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": node.Name})
	if err != nil {
		return err
	}
	// collect data first as that can fail
	records := make([]shuffleRecord, 0)
	for _, pod := range podList.Items {
		for _, ownerRef := range pod.OwnerReferences {
			params := fetchParams{
				ctx:    ctx,
				client: k8sClient,
				ref: types.NamespacedName{
					Namespace: pod.Namespace,
					Name:      ownerRef.Name,
				},
			}
			var record shuffleRecord
			switch ownerRef.Kind {
			case "DaemonSet":
				record, err = fetchShuffleDaemonSet(params)
			case "ReplicaSet":
				record, err = fetchShuffleDeployment(params)
			case "StatefulSet":
				record, err = fetchShuffleStatefulSet(params)
			default:
				continue
			}
			if err != nil {
				return err
			}
			records = append(records, record)
		}
	}
	// actually record metrics, when no error can happen
	for _, record := range records {
		labels := prometheus.Labels{
			"owner":   record.owner,
			"profile": currentProfile,
		}
		shuffleCount.With(labels).Inc()
		shufflesPerReplica.With(labels).Add(record.perReplica)
	}
	return nil
}

func fetchShuffleDaemonSet(params fetchParams) (shuffleRecord, error) {
	var daemonSet appsv1.DaemonSet
	err := params.client.Get(params.ctx, params.ref, &daemonSet)
	if err != nil {
		return shuffleRecord{}, err
	}
	replicas := daemonSet.Status.NumberReady
	return shuffleRecord{owner: "daemon_set_" + daemonSet.Name, perReplica: 1 / float64(replicas)}, nil
}

func fetchShuffleDeployment(params fetchParams) (shuffleRecord, error) {
	var replicaSet appsv1.ReplicaSet
	err := params.client.Get(params.ctx, params.ref, &replicaSet)
	if err != nil {
		return shuffleRecord{}, err
	}
	if len(replicaSet.OwnerReferences) == 0 {
		replicas := int32(1)
		if replicaSet.Spec.Replicas != nil {
			replicas = *replicaSet.Spec.Replicas
		}
		return shuffleRecord{owner: "replica_set_" + replicaSet.Name, perReplica: 1 / float64(replicas)}, nil
	}
	for _, ownerRef := range replicaSet.OwnerReferences {
		if ownerRef.Kind == "Deployment" {
			var deployment appsv1.Deployment
			err := params.client.Get(params.ctx,
				types.NamespacedName{
					Namespace: replicaSet.Namespace,
					Name:      ownerRef.Name,
				},
				&deployment)
			if err != nil {
				return shuffleRecord{}, err
			}
			replicas := int32(1)
			if deployment.Spec.Replicas != nil {
				replicas = *deployment.Spec.Replicas
			}
			return shuffleRecord{owner: "deployment_" + deployment.Name, perReplica: 1 / float64(replicas)}, nil
		}
	}
	return shuffleRecord{}, fmt.Errorf("owner of replicaSet %s is not a deployment", replicaSet.Name)
}

func fetchShuffleStatefulSet(params fetchParams) (shuffleRecord, error) {
	var statefulSet appsv1.StatefulSet
	err := params.client.Get(params.ctx, params.ref, &statefulSet)
	if err != nil {
		return shuffleRecord{}, err
	}
	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}
	return shuffleRecord{owner: "stateful_set_" + statefulSet.Name, perReplica: 1 / float64(replicas)}, nil
}
