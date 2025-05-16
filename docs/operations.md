<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Operations

## Metrics
The maintenance-controller exposes metrics in the Prometheus format.
They are available at the `/metrics` endpoint on the HTTP server listening on the port specified by the `--metrics-addr` flag.
It defaults to `:8080`.
The notable metrics are:
- `maintenance_controller_shuffle_count`: Counts pods in DaemonSets, Deployments and StatefulSets, that were likely deleted as part of a mainteanance activity.
- `maintenance_controller_shuffles_per_replica`: Count of pods in DaemonSets, Deployments and StatefulSets, that were likely deleted as part of a maintenance activity, divided by the replica count when the event occurred.
- `maintenance_controller_transition_failure_count`: Count of state transition failures due to plugin errors.
The first two help determine the impact of maintenance activities on the workloads running on the cluster.

## Web UI
The maintenance-controller provides a web UI to visualize the state of maintenance profiles and nodes.
It is available at the `/` endpoint on the HTTP server listening on the port specified by the `--metrics-addr` flag.

## Kubernetes
The maintenance-controller creates Kubernetes events on nodes for each state transition.
These are visible in the `kubectl describe node` as well as `kubectl get events` output.
Also, the maintenance-controller logs all errors and informational messages to the standard output.

When multiple instances of the maintenance-controller are running in a cluster, the `--enable-leader-election` **must** be set.
Otherwise, the instances will interfere with each other.
