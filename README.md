# Maintenance Controller
![GitHub Workflow Status](https://img.shields.io/github/workflow/status/sapcc/maintenance-controller/Build%20and%20run%20tests)
[![Coverage Status](https://coveralls.io/repos/github/sapcc/maintenance-controller/badge.svg)](https://coveralls.io/github/sapcc/maintenance-controller)
![Docker Pulls](https://img.shields.io/docker/pulls/sapcc/maintenance-controller)

A Kubernetes controller to manage node maintenance.

## Table of Contents
- Motivation
- Concept
- Installation
- Configuration
  - Check Plugins
  - Notification Plugins
  - Trigger Plugins
- Support for VMware ESX maintenance
- Example configuration for flatcar update agents

## Motivation
Sometimes the nodes of a Kubernetes cluster need to be put into maintenance.
There exist several reasons, like having to update the node's operating system or the kubelet daemon.
Putting a node into maintenance requires to cordon and drain the node.
Stateful applications might have special constraints regarding their termination, which cannot be handled easily using Kubernetes "PreStopHooks" (e.g. High Availability scenarios).
In enterprise contexts, additional processes might influence, when a node maintenance is allowed to occur.

The maintenance controller supports enforcing maintenance processes, automating maintenance approvals and customization of termination logic.
It is built with flexibility in mind and should be adaptable to different environments and requirements.
This property is achieved with an extensible plugin system.

## Concept
Kubernetes nodes are modelled as finite state machines and can be in one of three states.
- Operational
- Maintenance Required
- In Maintenance

A node's current state is saved within a configurable node label.
Nodes transition to the state if a chain of configurable "check plugins" decides that the node's state should move on.
Such plugin chains can be configured for each state individually via maintenance profiles.
Cluster administrators can assign a maintenance profile to a node using a label.
Before the transition is finished a chain of "trigger plugins" can be invoked, which can perform any action related to termination or startup logic.
While a node is in a certain state, a chain of "notifications plugins" informs the cluster users and administrators regularly about the node being in that state.
Multiple plugins exist.
It is possible to check or alter labels, to be notified via Slack, ...

## Installation

Execute ```make deploy IMG=sapcc/maintenance-controller```.

## Configuration

There is a global configuration, which defines some general options, plugin instances and maintenance profiles.
The global configuration should be named ```./config/maintenance.yaml``` and should be placed relative to the controllers working directory preferably via a Kubernetes secret or a config map.
The basic structure looks like this:
```yaml
intervals:
  # defines after which duration a node should be checked again
  requeue: 200ms
  # defines after which duration a reminder notification should be send
  notify: 500ms
# plugin instances are the combination of a plugin and its configuration
instances:
  # the are no notification plugins configured here, but their configuration works the same way as for check and trigger plugins
  notify: null
  # check plugin instances
  check:
  # the list entries define the chosen plugin type
  - hasLabel:
      # name of the instance, which is used in the plugin chain configurations
      # do not use spaces or other special characters, besides the underscore, which is allowed
      name: transition
      # the configuration for the plugin. That block depends obviously on the plugin type
      config:
        key: transition
        value: "true"
  # trigger plugin instances
  trigger:
  - alterLabel:
      name: alter
      config:
        key: alter
        value: "true"
        remove: false
profiles:
  # define a maintenance profile called someprofile
  someprofile:
    # define the plugin chains for the operational state
    operational:
      # the exit condition for the operational state refers to the "transition" plugin instance defined in the instances section
      check: transition
      # the notification instances to invoke while in the operational state
      notify: somenotificationplugin
      # the trigger instances which are invoked when leaving the operational state
      trigger: alter
    # define the plugin chains for the maintenance-required state
    maintenance-required:
      # define chains as shown with the operational state
      check: null
      notify: null
      trigger: null
    # define plugin chains for the in-maintenance state
    in-maintenance:
      # check chains support boolean operations which evaluate multiple instances
      check: transition && !(a || b)
      # multiple notification instances can be used also
      notify: g && h
      # multiple trigger instances can be used also
      trigger: t && u
```
Chains be undefined or empty.
Trigger and Notification chains are configured by specifying the desired instance names separated by ```&&```, e.g. ```alter && othertriggerplugin```
Check chains be build using boolean expression, e.g. ```transition && !(a || b)```
To attach a maintenance profile to a node, the label ```cloud.sap/maintenance-profile=NAME``` has to be assigned the desired profile name.
If that label is not present on a node the controller will use the ```default``` profile, which does nothing at all.
The default profile can be reconfigured if it is defined within the config file.
Multiple profiles can be assigned to a single node be setting ```cloud.sap/maintenance-profile=NAME1--NAME2--NAME3--...```.
The operational state can then be left by the checks configured in all listed profiles.
Any progress for the maintenance-required and in-maintenance states can only made using the profile, which initial triggered the whole maintenance workflow.
That way specific maintenance workflows for different causes can be implemented.
The controllers state is tracked with the ```cloud.sap/maintenance-state``` label and the ```cloud.sap/maintenance-data``` annotation.

### Check Plugins
__hasAnnotation:__ Checks if a node has an annotation with the given key. Optionally asserts the annotation value.
```yaml
config:
  key: the annotation key, required
  value: the expected annotation value, if empty only the key is checked, optional
```
__hasLabel__ Checks if a node has a label with the given key. Optionally asserts the labels value.
```yaml
config:
  key: the annotation key, required
  value: the expected annotation value, if empty only the key is checked, optional
```
__condition__ Checks if a node condition has the defined status.
```yaml
config:
  type: the node conditions type (usually one of Ready, MemoryPressure, DiskPressure, PIDPressure or NetworkUnavailable)
  status: the expected condition status (usually one of True, False or Unknown)
```
__maxMaintenance:__ Checks that less than the specified amount of nodes are in the in-maintenance state. Due to optimistic concurrency control of the API-Server this check might return the wrong result if more than one node is reconciled at any given time.
```yaml
config:
  max: the limit of nodes that are in-maintenance
```
__stagger__: Checks that a certain duration has passed since the previous node passed. This is implemented with a `coordination.k8s.io/Lease`, which needs to be manually removed when the maintenance controller is removed from a cluster.
```yaml
config:
  duration: the duration to await, required
  leaseName: name of the lease, required
  leaseNamespace: namespace of the lease, required
```
__timeWindow:__ Checks if the current systemtime is within the specified weekly UTC time window.
```yaml
config:
  start: the timewindows start time in "hh:mm" format, required
  end: the timewindows end time in "hh:mm" format, required
  weekdays: weekdays when the time window is valid as array e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
```
__wait:__ Checks if a certain duration has passed since the last state transition
```yaml
config:
  duration: a duration according to the rules of golangs time.ParseDuration(), required
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

## Support for VMware ESX maintenance
See [here](esx/README.md).

## Example configuration for flatcar update agents
```yaml
intervals:
    requeue: 60s
    notify: 5h
instances:
    notify:
    - slack:
        name: approval_required
        config:
          hook: Your hook
          channel: Your channel
          message: |
            The node {{ .Node.Name }} requires maintenance. Manual approval is required.
            Approve to drain and reboot this node by running:
            `kubectl annotate node {{ .Node.Name }} cloud.sap/maintenance-approved=true`
    - slack:
        name: maintenance_started
        config:
          hook: Your hook
          channel: Your channel
          message: |
            Maintenance for node {{ .Node.Name }} has started.
    check:
    - hasAnnotation:
        name: reboot_needed
        config:
            key: flatcar-linux-update.v1.flatcar-linux.net/reboot-needed
            value: "true"
    - hasAnnotation:
        name: check_approval
        config:
            key: cloud.sap/maintenance-approved
            value: "true"
    trigger:
    - alterAnnotation:
        name: reboot-ok
        config:
            key: flatcar-linux-update.v1.flatcar-linux.net/reboot-ok
            value: "true"
    - alterAnnotation:
        name: remove_approval
        config:
            key: cloud.sap/maintenance-approved
            remove: true
    - alterAnnotation:
        name: remove_reboot_ok
        config:
            key: flatcar-linux-update.v1.flatcar-linux.net/reboot-ok
            remove: true
profiles:
  flatcar:
    operational:
      check: reboot_needed
    maintenance-required:
      check: check_approval
      notify: approval_required
      trigger: remove_approval && reboot-ok
    in-maintenance:
      check: "!reboot_needed"
      notify: maintenance_started
      trigger: remove_reboot_ok
```
