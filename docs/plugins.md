<!--
SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Plugins

## Check plugins

### hasAnnotation
Checks if a node has an annotation with the given key.
Optionally asserts the annotation value.
```yaml
config:
  key: the annotation key, required
  value: the expected annotation value, if empty only the key is checked, optional
```

### hasLabel
Checks if a node has a label with the given key.
Optionally asserts the labels value.
```yaml
config:
  key: the label key, required
  value: the expected label value, if empty only the key is checked, optional
```

### anyLabel
Checks that at least one node in the cluster has a label with the given key.
Optionally asserts that the label must match a certain value.
```yaml
config:
  key: the label key, required
  value: the expected label value, if empty only the key is checked, optional
```

### checkHypervisor
Checks if a key property of the hypervisor CRO of the node matches the expected value.
```yaml
config:
  <key>: <expected value>
```

### clusterSemver
Checks if a label containing a semantic version is less than the most up-to-date value in the cluster.
Requires the checked node to have the specified label.
```yaml
config:
  key: a valid label key, required
  profileScoped: do not check against the whole cluster, but against all nodes, which match the current profile, optional
```

### condition
Checks if a node condition has the defined status.
```yaml
config:
  type: the node conditions type (usually one of Ready, MemoryPressure, DiskPressure, PIDPressure or NetworkUnavailable), required
  status: the expected condition status (usually one of True, False or Unknown), required
```

### kubernikusCount
Checks that the node count on the Kubernetes API is greater or equal to the nodes specified on the Kubernikus API.
```yaml
config:
  cluster: Kubernikus cluster name, required
  cloudProviderSecret: Reference to a secret containing a cloudprovider.conf key, optional
    name: Name of such a secret
    namespace: Namespace of such a secret
```

### maxMaintenance
Checks that less than the specified amount of nodes are in the in-maintenance state.
Due to optimistic concurrency control of the API-Server this check might return the wrong result if more than one node is reconciled at any given time.
In the default configuration all nodes in the cluster are considered.
If only a certain group of nodes is relevant consider setting the `groupBy` option.
```yaml
config:
  max: the limit of nodes that are in-maintenance, required
  profile: if set only consider nodes which do have the specified profile, optional
  skipAfter: if set only considers nodes, for which the time since the last transition does not exceed the specified duration, optional
  # if set only considers nodes, which have the same value as the current reconciled node for the specified labels, optional
  groupBy:
  - firstLabel
  - secondLabel
  # deprecated, label will be appended to groupBy, if not already included
  # if set only considers nodes, which have the same value for the specified label, optional
  groupLabel: singleLabel
```

### prometheusInstant
Checks that the most recent value of a prometheus query satisfies a given expression.
```yaml
config:
  url: prometheus url
  query: prometheus query, that yields a vector with exactly a single value
  expr: comparison where 'value' is fetched from prometheus, e.g. 'value <= 1'
```

### stagger
Checks that a certain duration has passed since a previous node passed.
This is implemented with `coordination.k8s.io/Lease`s, which needs to be manually removed when the maintenance controller is removed from a cluster.
```yaml
config:
  duration: the duration to await according to the rules of golangs time.ParseDuration(), required
  leaseName: name prefix of the lease, required
  leaseNamespace: namespace of the lease, required
  parallel: the amount of leases to use, optional (defaults to 1)
```

### timeWindow
Checks if the current systemtime is within the specified weekly UTC time window.
```yaml
config:
  start: the timewindows start time in "hh:mm" format, required
  end: the timewindows end time in "hh:mm" format, required
  weekdays: weekdays when the time window is valid as array, e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
  exclude: month/day combinations as array, when maintenances are not allowed to occur, e.g. ["Dec 24", "Oct 31"], optional
```

### wait
Checks if a certain duration has passed since the last state transition.
```yaml
config:
  duration: a duration according to the rules of golangs time.ParseDuration(), required
```

### waitExclude
Checks if a certain duration has passed since the last state transition, while time does not progress on excluded days.
This likely to have some inaccuracies, e.g. leap seconds due to the involved math.
```yaml
config:
  duration: a duration according to the rules of golangs time.ParseDuration(), required
  exclude: weekdays when the time does not progress, e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
```

### affinity
Pods are rescheduled, when a node is drained.
While maintaining a whole cluster it is possible that are rescheduled onto nodes, which are subject to another drain soon.
This effect can be reduced by specifying a preferred node affinity towards nodes in the operational state.
The affinity check plugin prefers to send nodes into maintenance, which do not have pods matching exactly the node affinity below, so nodes with non-critical pods are maintained first to provide operational nodes for critical workloads.
This is not perfect, because nodes enter the maintenance-required over a certain duration, but better than ignoring such scheduling issues at all.
__An instance of this check plugin can only be used for the maintenance-required state.__
```yaml
config:
  minOperational: minimum count of nodes that need to operational in the same profile to pass, ignoring the logic outlined above, setting it to zero (default) disables this, optional
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

### nodeCount
Checks if the cluster has at least the specified of nodes.
```yaml
config:
  count: the amount of nodes to present at least
```

## Trigger plugins

### alterAnnotation
Adds, changes or removes an annotation.
```yaml
config:
  key: the annotations key, required
  value: the value to set, optional
  remove: boolean value, if true the annotation is removed, if false the annotation is added or changed, optional
```

### alterFinalizer
Adds or removes a finalizer.
```yaml
config:
  key: the finalizer key to add or remove, required
  remove: boolean value, if true the finalizer is removed, if false the finalizer is added, optional
```

### alterLabel
Adds, changes or removes a label.
```yaml
config:
  key: the labels key, required
  value: the value to set, optional
  remove: boolean value, if true the label is removed, if false the label is added or changed, optional
```

### alterHypervisor
Alters a property of the hypervisor CRO of the node.
```yaml
config:
  <key>: <new value>
```

### eviction
Cordons, uncordons or drains a node.
Ensure to run an instance with the `cordon` action before running an instance with the `drain` action.
```yaml
config:
  action: one of "cordon", "uncordon" or "drain", required
  deletionTimeout: how long to wait for pod removal to succeed during drain, optional
  evictionTimeout: how long to retry creation of pod evictions for drain, optional
  forceEviction: if true and eviction does not remove all pods, delete them afterwards for deletionTimeout, optional
```

## Notification plugins

### mail
Sends an e-mail
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

### slack
Sends a slack message
```yaml
config:
  hook: an incoming slack webhook, required
  channel: the channel which the message should be send to, required
  message: the content of the slack message, this supports golang templating e.g. {{ .State }} to get the current state as string or {{ .Node }} to access the node object, required
```

### slackThread
Sends slack messages and groups them in a thread if the given lease did not expire
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

## Notification schedules

### oneshot
Notifies once after a state change if the configured delay passes.
```yaml
type: oneshot
config:
  delay: a duration according to the rules of golangs time.ParseDuration(), defaults to 0, optional
```

### periodic
Notifies after a state change and when the specified interval passed since the last notification if the node is currently not in the operational state.
This reflects the old implicit notification behavior.
```yaml
type: periodic
config:
  interval: a duration according to the rules of golangs time.ParseDuration(), required
```

### scheduled
Notifies at a certain time only on specified weekdays.
```yaml
type: scheduled
config:
  instant: the point in time, when the notification should be sent, "hh:mm" format, required
  weekdays: weekdays when notification should be sent, e.g. [monday, tuesday, wednesday, thursday, friday, saturday, sunday], required
```
