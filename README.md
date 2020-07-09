# Maintenance Controller
A Kubernetes controller to manage node maintenance.

## Table of Contents
- Motivation
- Concept
- Installation
- Configuration
  - Check Plugins
  - Notification Plugins
  - Trigger Plugins

## Motivation
Sometimes the nodes of a Kubernetes cluster need to be put into maintenance.
There exist several reason, like updating the node's operation system or updating the kubelet daemon.
Putting a node into maintenance requires to cordon and drain the node.
Stateful applications might have special constraints regarding their termination, which cannot be handled easily using Kubernetes "PreStopHooks" (e.g. High Availability scenarios).
In enterprise contexts additional processes might influence, when a node maintenance is allowed to occur.

The maintenance controller supports enforcing maintenance processes, automating maintenance approvals and customization of termination logic.
It is build with flexibility in mind and should be adaptable to different environments and requirements.
This property is achieved with an extensible plugin systems

## Concept
Kubernetes nodes are modelled as finite state machines and can be in one of three states.
- Operational
- Maintenance Required
- In Maintenance

A node's current state is saved within a configurable node label.
Nodes transition to the state if a chain of configurable "check plugins" decides that the node's state should move on.
Such plugin chains can be configured for each state individually via annotations.
Before the transition is finished a chain of "trigger plugins" can be invoked, which can perform any action related to termination or startup logic.
While a node is in a certain state a chain of "notifications plugins" informs the cluster users and adminstrators regulary about the node being in that state.
Multiple plugins exist.
It is possible to check labels, to alter labels, to be notified via Slack, ...

## Installation

Execute ```make deploy IMG=sapcc/maintenance-controller```.

## Configuration

There is a global configuration, which defines some general options, and the configuration of plugin chains via node annotations.
The global configuration should be named ```./config/maintenance.yaml``` and should be placed relative to the controllers working directory preferably via a Kubernetes secret or a config map.
The basic structure looks like this:
```yaml
intervals:
  # defines after which duration a node should be checked again
  requeue: 200ms
  # defines after which duration a reminder notification should be send
  notify: 500ms
keys:
  # defines the label key which the controller uses to save the current state of a node
  state: state
  # defines an annotation prefix which the controller uses to identify the configured plugin chains for a node
  chain: chain
# plugin instances are the combination of a plugin and its configuration
instances:
  # the are no notification plugins configured here, but their configuration works the same way as for check and trigger plugins
  notify: null
  # check plugin instances
  check:
  # the list enttries define the chosen plugin type
  - hasLabel:
      # name of the instance, which is used in the plugin chain configurations. Do not use spaces or other special characters
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
```
Specified instances can than be used to configure node specific transition behavior by defining plugin chains.
The controller looks up annotations of the form ```prefix-state-plugintype``` where ```prefix``` is the configured prefix of the global configuration, ```state``` is one of ```operational, required or in-maintenance``` and ```plugintype``` is one of ```check, trigger or notify```.
So there are nine chains to be configured if desired.
Chains be undefined or empty.
Trigger and Notification chains are configured by specifing the desired instance names sperated by ```&&```, e.g. ```prefix-operational-trigger=alter && othertriggerplugin```
Check chains be build using boolean expression, e.g. ```prefix-in-maintenance-check=transition && !(a || b)```

### Check Plugins
__hasAnnotation:__ Checks if a node has an annotation with the given key. Optionally asserts the annotation value.
```yaml
config:
  key: the annotation key, required
  value: the expect annotation value, if empty only the key is checked, optional
```
__hasLabel__ Checks if a node has a label with the given key. Optionally asserts the labels value.
```yaml
config:
  key: the annotation key, required
  value: the expect annotation value, if empty only the key is checked, optional
```
__maxMaintenance:__ Checks that less than the specified amount of nodes are in the in-maintenance state. Due to optimistic concurrency control of the API-Server this check might return the wrong result if more than one node is reconciled at any given time.
```yaml
config:
  max: the limit of nodes that are in-maintenance
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
  identity: the identity used for authentification against the smtp server, optional
  subject: the subject of the mail
  message: the content of the mail, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
  password: the password used for authentification against the smtp server, optional
  to: array of recipients, required
  user: the user used for authentification against the smtp server, optional
```
__slack__: Sends a slack message
```yaml
config:
  hook: an incoming slack webhook, required
  channel: the channel which the message should be send to, required
  message: the content of the slack message, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
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
