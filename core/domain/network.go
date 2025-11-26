package domain

import "time"

// Network represents a Docker network.
type Network struct {
	Name       string
	ID         string
	Created    time.Time
	Scope      string // local, global, swarm
	Driver     string
	EnableIPv6 bool
	IPAM       IPAM
	Internal   bool
	Attachable bool
	Ingress    bool
	Containers map[string]EndpointResource
	Options    map[string]string
	Labels     map[string]string
}

// IPAM represents IP Address Management configuration.
type IPAM struct {
	Driver  string
	Options map[string]string
	Config  []IPAMConfig
}

// IPAMConfig represents IPAM configuration for a network.
type IPAMConfig struct {
	Subnet     string
	IPRange    string
	Gateway    string
	AuxAddress map[string]string
}

// EndpointResource contains network endpoint resources.
type EndpointResource struct {
	Name        string
	EndpointID  string
	MacAddress  string
	IPv4Address string
	IPv6Address string
}

// NetworkListOptions represents options for listing networks.
type NetworkListOptions struct {
	Filters map[string][]string
}

// NetworkConnectOptions represents options for connecting a container to a network.
type NetworkConnectOptions struct {
	Container      string
	EndpointConfig *EndpointSettings
}
