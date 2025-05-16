<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Motivation

Nodes, which make up Kubernetes clusters, require maintenance to stay secure and up-to-date.
Maintenance activities, like OS patching and Kubernetes upgrades, are tedious, recurring tasks that can disrupt cluster workloads.
Performing these tasks manually is already time-consuming by itself, but aligning maintenance activities with stakeholders is challenging as well.
This is where the maintenance-controller comes in.
It allows you to encode the requirements around maintenance activities in a declarative way and execute consistently across your clusters.
The actual execution of maintenance activities is usually passed to external controllers.
