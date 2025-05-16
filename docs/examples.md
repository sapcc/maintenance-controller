<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Examples

## Flatcar Linux Update Agent
The following example shows how to configure the maintenance-controller to perform OS patching for Flatcar Linux nodes.
The example requires the [Flatcar Linux Update Agent](https://github.com/flatcar/flatcar-linux-update-operator) is installed in the cluster.

### Configuration

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

### Explanation

Nodes in the `flatcar` profile transition from `operational` to `maintenance-required` if they require a reboot.
Administrators are notified that manual approval is required.
Then nodes wait for manual approval before transitioning to the `in-maintenance` state.
The approval is given by setting the `cloud.sap/maintenance-approved` annotation to `true`.
After maintenance is complete, nodes transition back to the `operational` state.
