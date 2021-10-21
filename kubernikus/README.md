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
__Ensure to disable Kuberniku's own Servicing controller.__
There is no synchronization between the Servicing controller and the maintenance-controller.
The main configuration should be placed in `./config/kubernikus.yaml`
```yaml
intervals:
  requeue: 30s # Minimum frequency to check for node replacements
  # Defines how long and frequent to check for pod deletions while draining
  podDeletion:
    period: 20s
    timeout: 5m
```
Also OpenStack credentials have to provided to delete the virtual machine backing a Kubernikus node.
These have to be placed in `./provider/cloudprovider.conf`.
Usually its enough to mount the `cloud-config` secret of the `kube-system` namespace of a Kubernikus cluster into the container.
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
