// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"net"
	"net/netip"
	"testing"
	"time"

	networktypes "github.com/moby/moby/api/types/network"
)

// Tests targeting surviving CONDITIONALS_BOUNDARY and NEGATION mutations in convert.go

// mustMAC parses a MAC address for test fixtures; network.HardwareAddr
// replaced the plain string the frozen SDK used.
func mustMAC(s string) networktypes.HardwareAddr {
	hw, err := net.ParseMAC(s)
	if err != nil {
		panic(err)
	}
	return networktypes.HardwareAddr(hw)
}

func TestConvertFromNetworkResource_IPAMConditions(t *testing.T) {
	// Targeting line 158: if n.IPAM.Driver != "" || len(n.IPAM.Config) > 0
	// Test both branches: IPAM with/without driver, with/without config

	testCases := []struct {
		name       string
		input      networktypes.Summary
		wantIPAM   bool
		wantDriver string
		desc       string
	}{
		// Test IPAM Driver condition (n.IPAM.Driver != "")
		{
			name: "empty_driver_empty_config",
			input: networktypes.Summary{
				Network: networktypes.Network{
					Name:   "test-network",
					ID:     "abc123",
					Driver: "bridge",
					IPAM: networktypes.IPAM{
						Driver:  "",
						Config:  nil,
						Options: nil,
					},
				},
			},
			wantIPAM:   false,
			wantDriver: "",
			desc:       "Empty driver and empty config should not set IPAM",
		},
		{
			name: "non_empty_driver_empty_config",
			input: networktypes.Summary{
				Network: networktypes.Network{
					Name:   "test-network",
					ID:     "abc123",
					Driver: "bridge",
					IPAM: networktypes.IPAM{
						Driver:  "default",
						Config:  nil,
						Options: nil,
					},
				},
			},
			wantIPAM:   true,
			wantDriver: "default",
			desc:       "Non-empty driver should set IPAM even with empty config",
		},
		// Test IPAM Config condition (len(n.IPAM.Config) > 0)
		{
			name: "empty_driver_non_empty_config",
			input: networktypes.Summary{
				Network: networktypes.Network{
					Name:   "test-network",
					ID:     "abc123",
					Driver: "bridge",
					IPAM: networktypes.IPAM{
						Driver: "",
						Config: []networktypes.IPAMConfig{
							{Subnet: netip.MustParsePrefix("172.17.0.0/16")},
						},
						Options: nil,
					},
				},
			},
			wantIPAM:   true,
			wantDriver: "",
			desc:       "Non-empty config should set IPAM even with empty driver",
		},
		{
			name: "both_driver_and_config",
			input: networktypes.Summary{
				Network: networktypes.Network{
					Name:   "test-network",
					ID:     "abc123",
					Driver: "bridge",
					IPAM: networktypes.IPAM{
						Driver: "custom",
						Config: []networktypes.IPAMConfig{
							{Subnet: netip.MustParsePrefix("10.0.0.0/8"), Gateway: netip.MustParseAddr("10.0.0.1")},
						},
						Options: nil,
					},
				},
			},
			wantIPAM:   true,
			wantDriver: "custom",
			desc:       "Both driver and config should set IPAM",
		},
		// Boundary test: exactly one config item (len > 0)
		{
			name: "single_config_item",
			input: networktypes.Summary{
				Network: networktypes.Network{
					Name:   "test-network",
					ID:     "abc123",
					Driver: "bridge",
					IPAM: networktypes.IPAM{
						Driver: "",
						Config: []networktypes.IPAMConfig{
							{Subnet: netip.MustParsePrefix("192.168.0.0/24")},
						},
					},
				},
			},
			wantIPAM:   true,
			wantDriver: "",
			desc:       "Single config item (boundary) should set IPAM",
		},
		// Zero config items
		{
			name: "zero_config_items",
			input: networktypes.Summary{
				Network: networktypes.Network{
					Name:   "test-network",
					ID:     "abc123",
					Driver: "bridge",
					IPAM: networktypes.IPAM{
						Driver: "",
						Config: []networktypes.IPAMConfig{},
					},
				},
			},
			wantIPAM:   false,
			wantDriver: "",
			desc:       "Zero config items should not set IPAM",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := convertFromNetworkResource(&tc.input)

			hasIPAM := result.IPAM.Driver != "" || len(result.IPAM.Config) > 0
			if hasIPAM != tc.wantIPAM {
				t.Errorf("%s: hasIPAM = %v, want %v", tc.desc, hasIPAM, tc.wantIPAM)
			}

			if result.IPAM.Driver != tc.wantDriver {
				t.Errorf("%s: IPAM.Driver = %q, want %q", tc.desc, result.IPAM.Driver, tc.wantDriver)
			}
		})
	}
}

func TestConvertFromNetworkInspect_IPAMConditions(t *testing.T) {
	// Targeting line 207: if n.IPAM.Driver != "" || len(n.IPAM.Config) > 0
	// This is the same condition in convertFromNetworkInspect

	testCases := []struct {
		name       string
		ipam       networktypes.IPAM
		wantIPAM   bool
		wantDriver string
		desc       string
	}{
		{
			name: "empty_ipam",
			ipam: networktypes.IPAM{
				Driver: "",
				Config: nil,
			},
			wantIPAM:   false,
			wantDriver: "",
			desc:       "Empty IPAM should not be set",
		},
		{
			name: "only_driver",
			ipam: networktypes.IPAM{
				Driver: "custom",
				Config: nil,
			},
			wantIPAM:   true,
			wantDriver: "custom",
			desc:       "Only driver should set IPAM",
		},
		{
			name: "only_config",
			ipam: networktypes.IPAM{
				Driver: "",
				Config: []networktypes.IPAMConfig{
					{Subnet: netip.MustParsePrefix("10.0.0.0/8")},
				},
			},
			wantIPAM:   true,
			wantDriver: "",
			desc:       "Only config should set IPAM",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := &networktypes.Inspect{
				Network: networktypes.Network{
					Name:    "test-network",
					ID:      "abc123",
					Created: time.Now(),
					IPAM:    tc.ipam,
				},
			}

			result := convertFromNetworkInspect(input)

			hasIPAM := result.IPAM.Driver != "" || len(result.IPAM.Config) > 0
			if hasIPAM != tc.wantIPAM {
				t.Errorf("%s: hasIPAM = %v, want %v", tc.desc, hasIPAM, tc.wantIPAM)
			}

			if result.IPAM.Driver != tc.wantDriver {
				t.Errorf("%s: IPAM.Driver = %q, want %q", tc.desc, result.IPAM.Driver, tc.wantDriver)
			}
		})
	}
}

func TestConvertFromNetworkInspect_ContainersCondition(t *testing.T) {
	// Targeting line 223: if len(n.Containers) > 0

	testCases := []struct {
		name           string
		containers     map[string]networktypes.EndpointResource
		wantContainers bool
		desc           string
	}{
		{
			name:           "nil_containers",
			containers:     nil,
			wantContainers: false,
			desc:           "Nil containers in Inspect should not set map",
		},
		{
			name:           "empty_containers",
			containers:     map[string]networktypes.EndpointResource{},
			wantContainers: false,
			desc:           "Empty containers in Inspect should not set map",
		},
		{
			name: "one_container_boundary",
			containers: map[string]networktypes.EndpointResource{
				"c1": {
					Name:        "container1",
					EndpointID:  "ep1",
					MacAddress:  mustMAC("02:42:ac:11:00:02"),
					IPv4Address: netip.MustParsePrefix("172.17.0.2/16"),
				},
			},
			wantContainers: true,
			desc:           "Single container in Inspect should set map",
		},
		{
			name: "multiple_containers",
			containers: map[string]networktypes.EndpointResource{
				"c1": {Name: "c1", IPv4Address: netip.MustParsePrefix("172.17.0.2/16")},
				"c2": {Name: "c2", IPv4Address: netip.MustParsePrefix("172.17.0.3/16")},
				"c3": {Name: "c3", IPv4Address: netip.MustParsePrefix("172.17.0.4/16")},
			},
			wantContainers: true,
			desc:           "Multiple containers in Inspect should set map",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			input := &networktypes.Inspect{
				Network: networktypes.Network{
					Name:    "test-network",
					ID:      "abc123",
					Created: time.Now(),
				},
				Containers: tc.containers,
			}

			result := convertFromNetworkInspect(input)

			hasContainers := len(result.Containers) > 0
			if hasContainers != tc.wantContainers {
				t.Errorf("%s: hasContainers = %v, want %v", tc.desc, hasContainers, tc.wantContainers)
			}

			if tc.wantContainers && len(result.Containers) != len(tc.containers) {
				t.Errorf("%s: container count = %d, want %d",
					tc.desc, len(result.Containers), len(tc.containers))
			}
		})
	}
}
