# Concepts

## Maintenance profiles
A maintenance profile is a declarative representation of a specific maintenance workflow.
It is advised to create a maintenance profile for each maintenance activity you want to perform on your nodes.
For example, you can create a maintenance profile for OS patching and another one for Kubernetes upgrades.

To assign a maintenance profile to a node, you add the `cloud.sap/maintenance-profile` label to the node with the name of the maintenance profile as the value.
Multiple profiles can be assigned to a node by separating the profile names with a double dash `--`.
Assuming the use-cases outlined above, you can assign the `os-patching` and `k8s-upgrade` maintenance profiles to a node by setting the label `cloud.sap/maintenance-profile: os-patching--k8s-upgrade`.
Removing a profile from a node is done by removing the label from the node, which will usually prevent related maintenance activities from being executed.

## Profiles are finite state machines
The maintenance-controller models profiles as finite state machines (FSM) to represent the lifecycle of maintenance activities.
The FSM has the following states:
- Operational: The node is operational and running workloads.
- Maintenance-Required: The profile requires maintenance to be performed on the node.
- In-Maintenance: The node is undergoing maintenance.

The FSM transitions between states based on the maintenance profile's configuration and the node's current state.
An FSM is tracked for each maintenance profile assigned to a node.
These FSMs are handled independently mostly.
The exception is that a node can only be in-maintenance for one profile at a time.

The `cloud.sap/maintenance-state` label on a node indicates the most crucial state of all profiles assigned to the node.
That label is only informational.
The actual state tracked by the maintenance-controller is stored in the `cloud.sap/maintenance-state` annotation.

## The default profile
The default profile is a special maintenance profile that is assigned to any node that does not have a maintenance profile label.
It does nothing by default, but you can reconfigure it to perform a maintenance workflow on all nodes.
Just include a profile with the name `default` in the configuration file.

## State transitions
State transitions are configured per profile.
For each state you can specify the next state and the conditions that need to be met for the transition to happen.
The latter is achieved by defining a predicate, which consists of check plugin instances.
(The documentation refers to these predicate as chains.)
When the transition is done, the trigger plugin instances are executed, which interact with the cluster.
The following is an example of a state transition configuration:

```yaml
maintenance-required:
  transitions:
  - check: check_approval
    trigger: remove_approval && reboot-ok
    next: in-maintenance
```

## Plugins and plugin instances
Plugins are the building blocks of maintenance profiles.
They are used to define the conditions and actions of state transitions.
A plugin instance is a specific named configuration of a plugin.
All plugins are described in the [plugins](plugins.md) documentation.
For instance, the `hasLabel` plugin checks if a node has a specific label.
A plugin instance of the `hasLabel` plugin could check if a node has the label `maintenance-approved` set to `true`, which could be used to manually approve maintenance activities.
The following is an example of a plugin instance configuration:

```yaml
check:
- type: hasLabel
  name: check_approval
  config:
    key: maintenance-approved
    value: "true"
```

## Notifications
The maintenance-controller can send notifications to external systems such as Slack or email.
Notifications are not triggered by state transitions, but are bound to the state a profile is in.
The actual delivery of notifications is determined by notification schedules, which are attached to notification plugin instances.
Schedules allow you to send notifications at specific times or intervals for example.
Available schedules are described in the [plugins](plugins.md) documentation.
The following is an example of a notification plugin instance configuration:

```yaml
notify:
- type: slack
  name: notify_slack
  config:
    hook: https://...
  schedule:
    type: periodic
    config:
      interval: 24h
```
