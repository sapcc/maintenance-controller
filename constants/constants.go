// SPDX-FileCopyrightText: 2025 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package constants

const (
	// Generic constants.
	TrueStr string = "true"

	// Name of the leader election leases.
	LeaderElectionID string = "maintenance-controller-leader-election.cloud.sap"

	// Label key that holds the physical ESX host.
	HostLabelKey string = "kubernetes.cloud.sap/host"

	// Label key that holds the region and availability zone.
	FailureDomainLabelKey string = "failure-domain.beta.kubernetes.io/zone"

	// Maintenance Controller constants
	// DefaultProfileName is the name of the default maintenance profile.
	DefaultProfileName string = "default"

	// ConfigFilePath is the path to the configuration file.
	MaintenanceConfigFilePath string = "./config/maintenance.yaml"

	// StateLabelKey is the full label key, which the controller attaches the node state information to.
	StateLabelKey string = "cloud.sap/maintenance-state"

	// ProfileLabelKey is the full label key, where the user can attach profile information to a node.
	ProfileLabelKey string = "cloud.sap/maintenance-profile"

	// LogDetailsLabelKey is the full label key, that defines if details of checks, notifications, ... should be logged.
	LogDetailsLabelKey string = "cloud.sap/maintenance-log-details"

	// DataAnnotationKey is the full annotation key, to which the controller serializes internal data.
	DataAnnotationKey string = "cloud.sap/maintenance-data"

	// ESX controller constants
	// ConfigFilePath is the path to the configuration file.
	EsxConfigFilePath string = "config/esx.yaml"

	// Label key that holds whether a nodes esx host in maintenance or not.
	EsxMaintenanceLabelKey string = "cloud.sap/esx-in-maintenance"

	// Label key that holds whether a node can rebootet if the hosting ESX is set into maintenance.
	EsxRebootOkLabelKey string = "cloud.sap/esx-reboot-ok"

	// Label key that holds the esx version.
	EsxVersionLabelKey string = "cloud.sap/esx-version"

	// Annotation key that holds whether this controller started rebooting the node.
	EsxRebootInitiatedAnnotationKey string = "cloud.sap/esx-reboot-initiated"

	// Kubernikus related constants.
	// Label key that holds whether a kubelet needs to be updated.
	KubeletUpdateLabelKey = "cloud.sap/kubelet-needs-update"

	// Label key that holds if a node should be delete from nova.
	DeleteNodeLabelKey = "cloud.sap/delete-node"

	// Path to cloudprovider.conf.
	CloudProviderConfigFilePath string = "./provider/cloudprovider.conf"

	// Path to kubernikus.yaml.
	KubernikusConfigFilePath string = "./config/kubernikus.yaml"
)
