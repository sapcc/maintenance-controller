// SPDX-FileCopyrightText: 2020 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package esx

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/vmware/govmomi"
)

// Specifies the string in a vCenter URL, which is replaced by the availability zone.
const AvailabilityZoneReplacer string = "$AZ"

type Config struct {
	Intervals struct {
		Check struct {
			Jitter float64       `config:"jitter" validate:"min=0.001"`
			Period time.Duration `config:"period" validate:"required"`
		} `config:"check" validate:"required"`
		PodDeletion struct {
			Period  time.Duration
			Timeout time.Duration
		} `config:"podDeletion" validate:"required"`
		PodEviction struct {
			Period  time.Duration `config:"period" validate:"required"`
			Timeout time.Duration `config:"timeout" validate:"required"`
			Force   bool          `config:"force"`
		} `config:"podEviction" validate:"required"`
		VMShutdown struct {
			Period  time.Duration `config:"period" validate:"required"`
			Timeout time.Duration `config:"timeout" validate:"required"`
		} `config:"vmShutdown" validate:"required"`
	} `config:"intervals" validate:"required"`
	Alarms   []string
	VCenters VCenters `config:"vCenters" validate:"required"`
}

func (c *Config) AlarmsAsSet() map[string]struct{} {
	alarms := make(map[string]struct{})
	for _, alarm := range c.Alarms {
		alarms[alarm] = struct{}{}
	}
	return alarms
}

type Credential struct {
	Username string `config:"username" validate:"required"`
	Password string `config:"password"`
}

// VCenters contains connection information to regional vCenters.
type VCenters struct {
	// URL to regional vCenters with the availability zone replaced by AvailabilityZoneReplacer.
	Template string `config:"templateUrl" validate:"required"`
	// If true the vCenters certificates are not validated.
	Insecure bool `config:"insecure"`
	// Pair of credentials per availability zone.
	Credentials map[string]Credential `config:"credentials" validate:"required"`
	// Cache of vCenter clients per AZ.
	cache map[string]*govmomi.Client `config:",ignore"`
}

// Gets an URL to connect to a vCenters in a specific availability zone.
func (vc *VCenters) URL(availabilityZone string) (*url.URL, error) {
	withAZ := strings.ReplaceAll(vc.Template+"/sdk", AvailabilityZoneReplacer, availabilityZone)
	vCenterURL, err := url.Parse(withAZ)
	if err != nil {
		return nil, err
	}
	cred, ok := vc.Credentials[availabilityZone]
	if !ok {
		return nil, fmt.Errorf("no vCenter credentials have been provided for availability zone %v", availabilityZone)
	}
	vCenterURL.User = url.UserPassword(cred.Username, cred.Password)
	return vCenterURL, nil
}

// Returns a ready to use vCenter client for the given availability zone.
func (vc *VCenters) Client(ctx context.Context, availabilityZone string) (*govmomi.Client, error) {
	if vc.cache == nil {
		vc.cache = make(map[string]*govmomi.Client)
	}
	client, ok := vc.cache[availabilityZone]
	if ok {
		return client, nil
	}
	client, err := vc.makeClient(ctx, availabilityZone)
	if err != nil {
		return nil, err
	}
	vc.cache[availabilityZone] = client
	return client, nil
}

func (vc *VCenters) makeClient(ctx context.Context, availabilityZone string) (*govmomi.Client, error) {
	vcURL, err := vc.URL(availabilityZone)
	if err != nil {
		return nil, fmt.Errorf("failed to render vCenter URL: %w", err)
	}
	client, err := govmomi.NewClient(ctx, vcURL, vc.Insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create vCenter client: %w", err)
	}
	return client, nil
}

func (vc *VCenters) ClearCache(ctx context.Context, log logr.Logger) {
	for _, client := range vc.cache {
		// try logout, which should clean some resources on the vCenter
		err := client.Logout(ctx)
		if err != nil {
			log.Error(err, "failed to logout of vCenter")
		}
	}
	vc.cache = make(map[string]*govmomi.Client)
}
