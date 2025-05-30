<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Kubernikus Controller
The Kubernikus controller does two things.
Firstly it regularly compares a node's kubelet version with the API server version.
If these do not match a node is labeled with `cloud.sap/kubelet-needs-update=true`.
Besides the name it also recognizes downgrades.
Secondly a node can be marked with `cloud.sap/delete-node` to cordon, drain and delete it from a [Kubernikus](https://github.com/sapcc/kubernikus) environment.
Be aware that a node being deleted is completely removed from the cluster and is in turn no longer influencing the maintenance-controllers logic although the node is unavailable.

Using the `cloud.sap/kubelet-needs-update` and `cloud.sap/delete-node` labels allows for tight integration with the main maintenance-controller to drive Kubernikus upgrades flexibly.

## Installation
The Kubernikus controller is bundled within the maintenance controller binary. It needs to be enabled using the `--enable-kubernikus-maintenance` flag.

## Configuration
There is no synchronization between Kubernikus servicing-controller and the maintenance-controller performing maintenances.
Recent Kubernikus versions bail out of servicing automatically if at least one node within a cluster has a `cloud.sap/maintenance-profile` label with a value.
The main configuration should be placed in `./config/kubernikus.yaml`
```yaml
intervals:
  requeue: 30s # Minimum frequency to check for node replacements
  # Defines how long and frequent to check for pod deletions while draining
  podDeletion:
    period: 20s
    timeout: 5m
  # Defines how long and frequent to try to evict pods
  podEviction:
    period: 20s
    timeout: 5m
    force: false # If true and evictions do not succeed do normal DELETE calls
cloudProviderSecret: # Optional reference to a secret containing a cloudprovider.conf key
  name: Name of such a secret
  namespace: Namespace of such a secret
```
Also OpenStack credentials have to provided to delete the virtual machine backing a Kubernikus node.
These have to be placed in `./provider/cloudprovider.conf`.
Usually its enough to mount the `cloud-config` secret of the `kube-system` namespace of a Kubernikus cluster into the container.
Alternatively, a reference to such a secret can be specified in the configuration and retrieved from the apiserver at runtime.
```ini
[Global]
auth-url="keystone endpoint url"
domain-name="kubernikus"
tenant-id="id"
username="user"
password="pw"
region="region"
```
After a node is deleted it should be replaced within some minutes by a new VM with the correct kubelet version.
A node is gone from maintenance-controllers perspective after deletion, although it might not be replaced yet.
Ensure to add some checks, e.g. the stagger check plugin, to avoid multiple nodes leaving the cluster one after another without their replacements being ready.
Also nodes need to be labeled again with `cloud.sap/maintenance-profile=...` after replacements.
This can be automated by configuring [kubernikus node pools](https://github.com/sapcc/kubernikus/blob/master/swagger.yml#L584).

Kubernikus nodes use Flatcar Linux under the hood, which need to be updated as well.
A full exemplary configuration might look like the following.
Don't forget to mark nodes with `cloud.sap/maintenance-profile=flatcar--kubelet`.
```yaml
intervals:
  requeue: 60s
  notify: 6h
instances:
  notify:
  - type: slackThread
    name: maintenance_flatcar
    config:
      token: "token"
      period: 12h
      leaseName: maintenance-controller-flatcar
      leaseNamespace: kube-system
      # the quotes here are relevant as slack channel names starting with # would render to YAML comment otherwise
      channel: "#channel"
      title: "Updating the operating system of nodes."
      message: '{{ .Node.Name }} will reboot now to update Flatcar Linux from version {{ index .Node.Labels "flatcar-linux-update.v1.flatcar-linux.net/version" }} to version {{ index .Node.Annotations "flatcar-linux-update.v1.flatcar-linux.net/new-version" }}'
    schedule:
      type: periodic
      config:
        interval: 24h
  - type: slackThread
    name: maintenance_kubelet
    config:
      token: "token"
      period: 12h
      leaseName: maintenance-controller-kubernikus
      leaseNamespace: kube-system
      # the quotes here are relevant as slack channel names starting with # would render to YAML comment otherwise
      channel: "#channel"
      title: "Updating kubelets."
      message: '{{ .Node.Name }} will be replaced for kubelet update.'
    schedule:
      type: periodic
      config:
        interval: 24h
  check:
  - type: hasAnnotation
    name: reboot_needed
    config:
      key: flatcar-linux-update.v1.flatcar-linux.net/reboot-needed
      value: "true"
  - type: hasLabel
    name: replace_needed
    config:
      key: cloud.sap/kubelet-needs-update
      value: "true"
  - type: maxMaintenance
    name: count
    config:
      max: 1
  - type: condition
    name: node_ready
    config:
      type: Ready
      status: "True"
  - type: stagger
    name: stagger
    config:
      duration: 8m
      leaseName: maintenance-controller-stagger
      leaseNamespace: kube-system
  trigger:
  - type: alterAnnotation
    name: reboot_ok
    config:
      key: flatcar-linux-update.v1.flatcar-linux.net/reboot-ok
      value: "true"
  - type: alterAnnotation
    name: remove_reboot_ok
    config:
      key: flatcar-linux-update.v1.flatcar-linux.net/reboot-ok
      remove: true
  - type: alterLabel
    name: delete_node
    config:
      key: cloud.sap/delete-node
      value: "true"
  - type: alterLabel
    name: remove_delete_node
    config:
      key: cloud.sap/delete-node
      remove: true
profiles:
- name: kubelet
  operational:
    transitions:
    - check: replace_needed
      next: maintenance-required
  maintenance-required:
    transitions:
    - check: "count && stagger"
      trigger: delete_node
      next: in-maintenance
  in-maintenance:
    notify: maintenance_kubelet
    # state technically never left due to node deletion
    transitions:
    - check: "!replace_needed"
      trigger: remove_delete_node
      next: operational
- name: flatcar
  operational:
    transitions:
    - check: reboot_needed
      next: maintenance-required
  maintenance-required:
    transitions:
    - check: "count && stagger"
      trigger: reboot_ok
      next: in-maintenance
  in-maintenance:
    notify: maintenance_flatcar
    transitions:
    - check: "!reboot_needed && node_ready"
      trigger: remove_reboot_ok
      next: operational
```
