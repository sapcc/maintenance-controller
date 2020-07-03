# Maintenance Controller
A Kubernetes controller to manage node maintenance.

## Table of Contents
- Motivation
- Concept
- Installation
- Configuration

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

Execute ```make deploy```.

## Configuration

There is a global configuration, which defines some general options, and the configuration of plugin chains via node annotations.
The global configuration should be named ```maintenance_config.yaml``` and should be placed in the controllers working directory preferably via a Kubernetes secret or a config map.
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
      # name of the instance, which is used in the plugin chain configurations
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
Spzified instances can than be used to configure node specific transition behavior by defining plugin chains.
The controller looks up annotations of the form ```prefix-state-plugintype``` where ```prefix``` is the configured prefix of the global configuration, ```state``` is one of ```operational, required or in-maintenance``` and ```plugintype``` is one of ```check, trigger or notify```.
So there are nine chains to be configured if desired.
Chains be undefined or empty.
Trigger and Notification chains are configured by specifing the desired instance names sperated by ```&&```, e.g. ```prefix-operational-trigger=alter && othercheckplugin```
Check chains be build using boolean expression, e.g. ```prefix-in-maintenance-check=transition && !(a || b)```