# ESX Controller
The ESX controller does two things for Kubernetes nodes running on virtual machines managed by a VMware vCenter.
Firstly it regularly checks whether a nodes underlying ESX host is or goes into maintenance mode.
If so the label `cloud.sap/esx-in-maintenance` is set to `true`.

Secondly, to complete entering maintenance mode all virtual machines on an ESX host need to be turned off.
By setting the `cloud.sap/esx-reboot-ok` label to `true` on every node (within the cluster) belonging to certain ESX host, which is entering maintenance mode, the controller will cordon, drain and shutdown these nodes (and will keep them shutdown).
When the ESX host leaves maintenance mode the controller will turn the nodes on and uncordon them.
This behavior only occurs, if the `cloud.sap/esx-reboot-initiated` annotation is set to `true`, so it does not interfere with other maintenance activities.
The `cloud.sap/esx-reboot-initiated` annotation is managed by the controller based on the `cloud.sap/esx-in-maintenance` and `cloud.sap/esx-reboot-ok` labels.

Using the `cloud.sap/esx-in-maintenance` label together with the `cloud.sap/esx-reboot-ok` label enables ESX maintenances to be managed flexibly with the "main" maintenance controller.

It is assumed that the nodes names equal the names of the hosting virtual machines.
The availability zone within a cloud region is assumed to be the last character of the `failure-domain.beta.kubernetes.io/zone` label.
The ESX hosts are to be tracked on relevant nodes using the `kubernetes.cloud.sap/host` label.

## Installation
The ESX controller is bundled within the maintenance controller binary. It needs to be enabled using the `--enable-esx-maintenance` flag.

## Configuration
To be placed in `./config/esx.yaml`.
```yaml
intervals:
  # Defines how frequent the controller will check for ESX hosts entering maintenance mode.
  check:
    jitter: 0.1 # required
    period: 5m # required
  # Defines how long and frequent to check for pod deletions while draining.
  podDeletion:
    period: 5s # required
    timeout: 2m # required
  # Defines how long to wait after a node has been drained.
  # As node shutdowns are performed in a loop it helps staggering them.
  stagger: 20s # optional
vCenters:
  # Defines the urls to vCenters in different availability zones.
  # $AZ is replaced with the single character availability zone.
  templateUrl: https://some-vcenter-url-$AZ # required
  # Defines if a vCenters certificates should be checked.
  insecure: # optional, defaults to false
  # Credentials for the vCenter per availability zone.
  credentials: # required
    a:
      username: user # required
      password: pass # required
```