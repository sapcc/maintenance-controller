// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"errors"
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

	transitionFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "maintenance_controller_transition_failure_count",
		Help: "Count of failed state transition evaluations due to plugin errors",
	}, []string{"profile"})
)

func RegisterMaintenanceMetrics() {
	metrics.Registry.MustRegister(shuffleCount, shufflesPerReplica, transitionFailures)
}

type shuffleRecord struct {
	owner      string
	perReplica float64
}

type fetchParams struct {
	client client.Client
	ref    types.NamespacedName
}

// Actually increment shuffle counters.
func RecordShuffles(ctx context.Context, k8sClient client.Client, node *v1.Node, currentProfile string) error {
	var podList v1.PodList
	if k8sClient == nil {
		return errors.New("kubernetes client is nil")
	}
	err := k8sClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": node.Name})
	if err != nil {
		return err
	}
	// collect data first as that can fail
	records := make([]shuffleRecord, 0)
	for _, pod := range podList.Items {
		for _, ownerRef := range pod.OwnerReferences {
			params := fetchParams{
				client: k8sClient,
				ref: types.NamespacedName{
					Namespace: pod.Namespace,
					Name:      ownerRef.Name,
				},
			}
			var record shuffleRecord
			switch ownerRef.Kind {
			case "DaemonSet":
				record, err = fetchShuffleDaemonSet(ctx, params)
			case "ReplicaSet":
				record, err = fetchShuffleDeployment(ctx, params)
			case "StatefulSet":
				record, err = fetchShuffleStatefulSet(ctx, params)
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
		labels := makeLabels(record.owner, currentProfile)
		shuffleCount.With(labels).Inc()
		shufflesPerReplica.With(labels).Add(record.perReplica)
	}
	return nil
}

func fetchShuffleDaemonSet(ctx context.Context, params fetchParams) (shuffleRecord, error) {
	var daemonSet appsv1.DaemonSet
	err := params.client.Get(ctx, params.ref, &daemonSet)
	if err != nil {
		return shuffleRecord{}, err
	}
	replicas := daemonSet.Status.NumberReady
	return shuffleRecord{owner: "daemon_set_" + daemonSet.Name, perReplica: 1 / float64(replicas)}, nil
}

func fetchShuffleDeployment(ctx context.Context, params fetchParams) (shuffleRecord, error) {
	var replicaSet appsv1.ReplicaSet
	err := params.client.Get(ctx, params.ref, &replicaSet)
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
			err := params.client.Get(ctx,
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

func fetchShuffleStatefulSet(ctx context.Context, params fetchParams) (shuffleRecord, error) {
	var statefulSet appsv1.StatefulSet
	err := params.client.Get(ctx, params.ref, &statefulSet)
	if err != nil {
		return shuffleRecord{}, err
	}
	replicas := int32(1)
	if statefulSet.Spec.Replicas != nil {
		replicas = *statefulSet.Spec.Replicas
	}
	return shuffleRecord{owner: "stateful_set_" + statefulSet.Name, perReplica: 1 / float64(replicas)}, nil
}

// Add zero to shuffle counters as prometheus dislikes missing metrics.
// This should require less requests than RecordShuffles().
func TouchShuffles(ctx context.Context, k8sClient client.Client, node *v1.Node, currentProfile string) error {
	var podList v1.PodList
	err := k8sClient.List(ctx, &podList, client.MatchingFields{"spec.nodeName": node.Name})
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		for _, ownerRef := range pod.OwnerReferences {
			var labels prometheus.Labels
			switch ownerRef.Kind {
			case "DaemonSet":
				labels = makeLabels("daemon_set_"+ownerRef.Name, currentProfile)
			case "StatefulSet":
				labels = makeLabels("stateful_set_"+ownerRef.Name, currentProfile)
			case "ReplicaSet":
				owner, err := fetchTouchReplicaSet(ctx, fetchParams{
					client: k8sClient,
					ref: types.NamespacedName{
						Name:      ownerRef.Name,
						Namespace: pod.Namespace,
					},
				})
				if err != nil {
					return err
				}
				labels = makeLabels(owner, currentProfile)
			default:
				continue
			}
			shuffleCount.With(labels).Add(0)
			shufflesPerReplica.With(labels).Add(0)
		}
	}
	return nil
}

func fetchTouchReplicaSet(ctx context.Context, params fetchParams) (string, error) {
	var replicaSet appsv1.ReplicaSet
	err := params.client.Get(ctx, params.ref, &replicaSet)
	if err != nil {
		return "", err
	}
	if len(replicaSet.OwnerReferences) == 0 {
		return "replica_set_" + params.ref.Name, nil
	}
	for _, ownerRef := range replicaSet.OwnerReferences {
		if ownerRef.Kind == "Deployment" {
			return "deployment_" + ownerRef.Name, nil
		}
	}
	return "", fmt.Errorf("owner of replicaSet %s is not a deployment", replicaSet.Name)
}

func makeLabels(owner, profile string) prometheus.Labels {
	return prometheus.Labels{
		"owner":   owner,
		"profile": profile,
	}
}

func RecordTransitionFailure(profile string) {
	transitionFailures.With(prometheus.Labels{"profile": profile}).Inc()
}
