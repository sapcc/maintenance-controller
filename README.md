# Maintenance Controller
![GitHub Workflow Status](https://img.shields.io/github/actions/workflow/status/sapcc/maintenance-controller/test_workflow.yaml?branch=master)
[![Coverage Status](https://coveralls.io/repos/github/sapcc/maintenance-controller/badge.svg)](https://coveralls.io/github/sapcc/maintenance-controller)
![Docker Pulls](https://img.shields.io/docker/pulls/sapcc/maintenance-controller)

A Kubernetes controller to manage node maintenance.
Serves roughly 50 production clusters across SAP Converged Cloud.

## Table of Contents
- Motivation
- Concept
- Installation
- Configuration
  - General
  - Format
  - Check Plugins
  - Notification Plugins
  - Notification Schedules
  - Trigger Plugins
- Additional integrations
- Example configuration for flatcar update agents

## Motivation
Sometimes nodes of a Kubernetes cluster need to be put into maintenance.
There exist several reasons, like having to update the node's operating system or the kubelet daemon.
Putting a node into maintenance requires to cordon and drain that node.
Stateful applications might have special constraints regarding their termination, which cannot be handled easily using Kubernetes "PreStopHooks" (e.g. High Availability scenarios).
In enterprise contexts, additional processes might influence, when a node maintenance is allowed to occur.

The maintenance controller supports enforcing maintenance processes, automating maintenance approvals and customization of termination logic.
It is built with flexibility in mind and should be adaptable to different environments and requirements.
This property is achieved with an extensible plugin system.

## Concept
Kubernetes nodes are modelled as finite state machines, which can be in one of the following three states:
- Operational
- Maintenance Required
- In Maintenance

A node's current state is shown in the `cloud.sap/maintenance-state` node label.
Nodes transition to the state if a chain of configurable "check plugins" decides that the node's state should move on.
Such plugin chains can be configured for each state individually via maintenance profiles.
Cluster administrators can assign a maintenance profile to a node using the `cloud.sap/maintenance-profile` label.
Before the transition is finished a chain of "trigger plugins" can be invoked, which can perform any action related to termination or startup logic.
While a node is in a certain state, a chain of "notifications plugins" informs the cluster users and administrators regularly about the node being in that state.
Multiple plugins exist, so one can check or alter labels, be notified via Slack and so on.

The maintenance-controller only does the decision making, whether a node can be maintained or not.
Currently, most actual maintenance actions like Cordoning, Draining and Rebooting nodes are not carried out by the maintenance-controller and are instead delegated to inbuilt or external other controllers.
Check out the additional integrations further down.

## Installation

Docker Images are on [DockerHub](https://hub.docker.com/r/sapcc/maintenance-controller).
A helm chart can be found [here](https://github.com/sapcc/helm-charts/tree/master/system/maintenance-controller).
Alternatively, execute ```make deploy IMG=sapcc/maintenance-controller```.

## Configuration

### General
The maintenance-controller contains multiple plugins, which are configurable themselves.
`checkLabel` for example needs to know, which label needs to checked for which value.
The combination of a plugin type like `checkLabel` and its specific configuration is referred to as an instance.
Notification instances require a schedule, which describes when and how often to notify about state changes, additionally.
These instances can be chained together to construct more complex check, trigger and notification actions.
In that regard plugin chains refer to instances being used in conjunction.

Profiles describe a single maintenance workflow each by specifying how a node moves through the state machine.
For each state a notification chain can be configured.
Also transitions have to be defined.
These consist of at least of a check chain and the state, which should follow next.
Optionally, a trigger chain can be configured to perform actions, when a node moves from one state into the next one.

### Debugging
By port-forwarding to the port specified by the `metrics-addr` flag (default `8080`) of the currently active maintenance-controller one can access a webview on `/`, which shows details about the state the maintenance-controller has.
It provides an overview about how many nodes are in a certain state regarding a certain profile.
Also, individual check chain evaluations can be checked.

### Format
There is a global configuration, which defines some general options, plugin instances and maintenance profiles.
The global configuration should be named `./config/maintenance.yaml` and should be placed relative to the controllers working directory preferably via a Kubernetes secret or a config map.
A secret is recommend as some plugins may need authentication data.
Environment variables can be interpolated in value positions using `${ENV_VAR}`.
The basic structure looks like the following:
```yaml
intervals:
  # defines the minimum duration after which a node should be checked again
  requeue: 200ms
# plugin instances are the combination of a plugin and its configuration
instances:
  # notification plugin instances
  notify:
  - type: slack # the plugin type
    name: somenotificationplugin
    config:
      hook: slack-webhook
      channel: "#the_channel"
      message: the message
    # notification schedule
    schedule:
      type: periodic
      config:
        interval: 24h
  # check plugin instances
  check:
  - type: hasLabel # the plugin type
    # name of the instance, which is used in the plugin chain configurations
    # do not use spaces or other special characters, besides the underscore, which is allowed
    name: transition
    # the configuration for the plugin. That block depends on the plugin type
    config:
      key: transition
      value: "true"
  # trigger plugin instances
  trigger:
  - type: alterLabel
    name: alter
    config:
      key: alter
      value: "true"
      remove: false
profiles:
# define a maintenance profile called someprofile
- name: someprofile
  # define the plugin chains for the operational state
  operational:
    # the notification instances to invoke while in the operational state
    notify: somenotificationplugin
    transitions:
      # the exit condition for the operational state refers to the "transition" plugin instance defined in the instances section
    - check: transition
      # the trigger instances which are invoked when leaving the operational state
      trigger: alter
      # the following state after passing checks and executing triggers
      next: maintenance-required
  # define the plugin chains for the maintenance-required state
  maintenance-required:
    # define chains as shown with the operational state
    notify: null
    transitions: null
  # define plugin chains for the in-maintenance state
  in-maintenance:
    # multiple notification instances can be used
    notify: g && h
    transitions:
      # check chains support boolean operations which evaluate multiple instances
    - check: "transition && !(a || b)"
      # multiple trigger instances can be used also
      trigger: t && u
```
Chains can be undefined or empty.
Trigger and Notification chains are configured by specifying the desired instance names separated by ```&&```, e.g. ```alter && othertriggerplugin```.
Check chains are build using boolean expressions, e.g. ```transition && !(a || b)```.
To attach a maintenance profile to a node, the label ```cloud.sap/maintenance-profile=NAME``` has to be assigned the desired profile name.
If that label is not present on a node the controller will use the ```default``` profile, which does nothing at all.
The default profile can be reconfigured, if it is defined within the config file.
Multiple profiles can be assigned to a single node by setting ```cloud.sap/maintenance-profile=NAME1--NAME2--NAME3--...```.
These profiles are then executed concurrently with the only constraint being that only one profile can be `in-maintenance` at any point in time.
That way specific maintenance workflows for different causes can be implemented.
The controllers state is tracked with the ```cloud.sap/maintenance-state``` label and the ```cloud.sap/maintenance-data``` annotation.

### Check Plugins
__hasAnnotation:__ Checks if a node has an annotation with the given key. Optionally asserts the annotation value.
```yaml
config:
  key: the annotation key, required
  value: the expected annotation value, if empty only the key is checked, optional
```
__hasLabel:__ Checks if a node has a label with the given key. Optionally asserts the labels value.
```yaml
config:
  key: the label key, required
  value: the expected label value, if empty only the key is checked, optional
```
__anyLabel__: Checks that at least one node in the cluster has a label with the given key. Optionally asserts that the label must match a certain value.
```yaml
config:
  key: the label key, required
  value: the expected label value, if empty only the key is checked, optional
```
__clusterSemver:__ Checks if a label containing a semantic version is less than the most up-to-date value in the cluster. Requires the checked node to have the specified label.
```yaml
clusterSemver:
  key: a valid label key, required
  profileScoped: do not check against the whole cluster, but against all nodes, which match the current profile, optional
```
__condition:__ Checks if a node condition has the defined status.
```yaml
config:
  type: the node conditions type (usually one of Ready, MemoryPressure, DiskPressure, PIDPressure or NetworkUnavailable), required
  status: the expected condition status (usually one of True, False or Unknown), required
```
__kubernikusCount:__ Checks that the node count on the Kubernetes API is greater or equal to the nodes specified on the Kubernikus API.
```yaml
config:
  cluster: Kubernikus cluster name, required
```
__maxMaintenance:__ Checks that less than the specified amount of nodes are in the in-maintenance state. Due to optimistic concurrency control of the API-Server this check might return the wrong result if more than one node is reconciled at any given time.
```yaml
config:
  max: the limit of nodes that are in-maintenance, required
  profile: if set only consider nodes which do have the specified profile, optional
```
__stagger__: Checks that a certain duration has passed since a previous node passed. This is implemented with `coordination.k8s.io/Lease`s, which needs to be manually removed when the maintenance controller is removed from a cluster.
```yaml
config:
  duration: the duration to await according to the rules of golangs time.ParseDuration(), required
  leaseName: name prefix of the lease, required
  leaseNamespace: namespace of the lease, required
  parallel: the amount of leases to use, optional (defaults to 1)
```
__timeWindow:__ Checks if the current systemtime is within the specified weekly UTC time window.
```yaml
config:
  start: the timewindows start time in "hh:mm" format, required
  end: the timewindows end time in "hh:mm" format, required
  weekdays: weekdays when the time window is valid as array, e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
  exclude: month/day combinations as array, when maintenances are not allowed to occur, e.g. ["Dec 24", "Oct 31"], optional
```
__wait:__ Checks if a certain duration has passed since the last state transition.
```yaml
config:
  duration: a duration according to the rules of golangs time.ParseDuration(), required
```
__waitExclude:__ Checks if a certain duration has passed since the last state transition, while time does not progress on excluded days. This likely to have some inaccuracies, e.g. leap seconds due to the involved math.
```yaml
config:
  duration: a duration according to the rules of golangs time.ParseDuration(), required
  exclude: weekdays when the time does not progress, e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
```
__affinity:__ Pods are rescheduled, when a node is drained. While maintaining a whole cluster it is possible that are rescheduled onto nodes, which are subject to another drain soon.
This effect can be reduced by specifying a preferred node affinity towards nodes in the operational state.
The affinity check plugin prefers to send nodes into maintenance, which do not have pods matching exactly the node affinity below, so nodes with non-critical pods are maintained first to provide operational nodes for critical workloads.
This is not perfect, because nodes enter the maintenance-required over a certain duration, but better than ignoring such scheduling issues at all.
__An instance of this check plugin can only be used for the maintenance-required state.__
```yaml
config: null
```
```yaml
nodeAffinity:
  preferredDuringSchedulingIgnoredDuringExecution:
  - weight: 1 # the weight is not relevant
    preference: # the preference has to match
      matchExpressions:
      - key: cloud.sap/maintenance-state
        operator: In
        values:
        - operational
```
__nodeCount:__ Checks if the cluster has at least the specified of nodes.
```yaml
config:
  count: the amount of nodes to present at least
```

### Notification Plugins
__mail__: Sends an e-mail
```yaml
config:
  auth: boolean value, which defines if the plugin should use plain auth or no auth at all, required
  address: address of the smtp server with port, required
  from: e-mail address of the sender, required
  identity: the identity used for authentication against the smtp server, optional
  subject: the subject of the mail
  message: the content of the mail, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
  password: the password used for authentication against the smtp server, optional
  to: array of recipients, required
  user: the user used for authentication against the smtp server, optional
```
__slack__: Sends a slack message
```yaml
config:
  hook: an incoming slack webhook, required
  channel: the channel which the message should be send to, required
  message: the content of the slack message, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
```
__slackThread__: Sends slack messages and groups them in a thread if the given lease did not expire
```yaml
config:
  token: slack api token, required
  channel: the channel which the message should be send to, required
  title: the content of the main slack message, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
  message: the content of the slack replies, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
  leaseName: name of the lease, required
  leaseNamespace: namespace of the lease, required
  period: after which period a new thread should be started, required
```
One can get the current profile in a template using `{{ .Profile.Current }}`.
Be careful about using it in an instance that is invoked during the `operational` state, as all profiles attached to a node are considered for notification.
`{{ .Profile.Last }}` can be used instead, which refers to profile that caused the last state transition.

### Notification Schedules
__periodic__: Notifies after a state change and when the specified interval passed since the last notification if the node is currently not in the operational state.
This reflects the old implicit notification behavior.
```yaml
type: periodic
config:
  interval: a duration according to the rules of golangs time.ParseDuration(), required
```
__scheduled__: Notifies at a certain time only on specified weekdays.
```yaml
type: scheduled
config:
  instant: the point in time, when the notification should be sent, "hh:mm" format, required
  weekdays: weekdays when notification should be sent, e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
```

### Trigger Plugins
__alterAnnotation:__ Adds, changes or removes an annotation
```yaml
config:
  key: the annotations key, required
  value: the value to set, optional
  remove: boolean value, if true the annotation is removed, if false the annotation is added or changed, optional
```
__alterLabel:__ Adds, changes or removes a label
```yaml
config:
  key: the labels key, required
  value: the value to set, optional
  remove: boolean value, if true the label is removed, if false the label is added or changed, optional
```

## Additional integrations
- Support for [VMware ESX maintenances](esx/README.md)
- Support for [Kubernikus](kubernikus/README.md)
- The maintenance controller exports a bunch of prometheus metrics, but especially
  - `maintenance_controller_shuffle_count`: Counts pods in DaemonSets, Deployments and StatefulSets, that were likely shuffled by a node send into maintenance
  - `maintenance_controller_shuffles_per_replica`: Count of pods in DaemonSets, Deployments and StatefulSets, that were likely shuffled by a node send into maintenance, divided by the replica count when the event occurred

## Example configuration for flatcar update agents
This example requires that the Flatcar-Linux-Update-Agent is present on the nodes.
```yaml
intervals:
  requeue: 60s
  notify: 5h
instances:
  notify:
  - type: slack
    name: approval_required
    config:
      hook: Your hook
      channel: Your channel
      message: |
        The node {{ .Node.Name }} requires maintenance. Manual approval is required.
        Approve to drain and reboot this node by running:
        `kubectl annotate node {{ .Node.Name }} cloud.sap/maintenance-approved=true`
    schedule:
      type: periodic
      config:
        interval: 24h
  - type: slack
    name: maintenance_started
    config:
      hook: Your hook
      channel: Your channel
      message: |
        Maintenance for node {{ .Node.Name }} has started.
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
  - type: hasAnnotation
    name: check_approval
    config:
      key: cloud.sap/maintenance-approved
      value: "true"
  trigger:
  - type: alterAnnotation
    name: reboot-ok
    config:
      key: flatcar-linux-update.v1.flatcar-linux.net/reboot-ok
      value: "true"
  - type: alterAnnotation
    name: remove_approval
    config:
      key: cloud.sap/maintenance-approved
      remove: true
  - type: alterAnnotation
    name: remove_reboot_ok
    config:
      key: flatcar-linux-update.v1.flatcar-linux.net/reboot-ok
      remove: true
profiles:
- name: flatcar
  operational:
    transitions:
    - check: reboot_needed
      next: maintenance-required
  maintenance-required:
    notify: approval_required
    transitions:
    - check: check_approval
      trigger: remove_approval && reboot-ok
      next: in-maintenance
  in-maintenance:
    notify: maintenance_started
    transitions:
    - check: "!reboot_needed"
      trigger: remove_reboot_ok
      next: operational
```
