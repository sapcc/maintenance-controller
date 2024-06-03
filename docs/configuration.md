# Configuration

For the refered terms, see the [concepts](concepts.md) documentation.

## Configuration file
The maintenance-controller is configured using a single YAML file.
Environment variables can be interpolated in value positions using `${ENV_VAR}`.
The default location for the configuration file is `./config/maintenance.yaml` relative to the working directory.

## Configuration structure
The configuration file consists of the following top-level keys:
- `intervals`: Configuration for the intervals at which the maintenance-controller checks the state of nodes.
- `instances`: Configuration for the maintenance-controller plugin instances.
- `profiles`: Configuration for the maintenance profiles.

### Intervals
The `intervals` key only contains a single key, `requeue`, which specifies the maximum duration between evaluating the state of a node.

```yaml
intervals:
  requeue: 5m
```

### Instances
The `instances` key has three subkeys, one for each type of plugin: `notify`, `check`, and `trigger`.
Each subkey contains a list of plugin instances.
The `config` key contains configuration specific to a plugin.
See the [plugins](plugins.md) documentation for more information.

```yaml
instances:
  notify:
  - type: slack
    name: notify_slack
    config:
      hook: https://...
    schedule:
      type: periodic
      config:
        interval: 24h
  check:
  - type: hasLabel
    name: check_approval
    config:
      key: maintenance-approved
      value: "true"
  trigger:
  - type: alterLabel
    name: remove_approval
    config:
      key: maintenance-approved
      remove: true
```

### Profiles
The `profiles` key contains a list of maintenance profiles.
Each profile has a name and a list of states.
Each state has a name, a list of transitions, and a list of notification instances.

```yaml
profiles:
- name: os-patching
  notify: notify_slack
  operational:
    transitions:
    - check: check_approval
      trigger: remove_approval
      next: maintenance-required
  maintenance-required:
    transitions:
    - check: check_approval
      trigger: remove_approval
      next: in-maintenance
  in-maintenance:
    transitions:
    - check: check_approval
      trigger: remove_approval
      next: operational
```

Chains can be undefined or empty.
Trigger and Notification chains are configured by specifying the desired instance names separated by `&&`, e.g. `alter && othertriggerplugin`.
Check chains are build using boolean expressions, e.g. `transition && !(a || b)`.

## Example configuration

```yaml
intervals:
  # defines the minimum duration after which a node should be checked again
  requeue: 2m
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
