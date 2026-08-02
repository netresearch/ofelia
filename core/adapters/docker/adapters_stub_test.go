// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package docker

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/netresearch/ofelia/core/domain"
	"github.com/netresearch/ofelia/core/ports"
)

// The system, image and network adapters translate between the domain types
// and the Docker SDK the same way the container adapter does, and their
// success branches were unreachable without a daemon for the same reason.
// These tests answer as a daemon would (see stubSDK in
// container_wrappers_test.go) so the translation is asserted directly.

func TestSystemServiceAdapter_Ping_MapsFields(t *testing.T) {
	t.Parallel()

	adapter := &SystemServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/_ping": func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Api-Version", "1.43")
			w.Header().Set("Ostype", "linux")
			w.Header().Set("Docker-Experimental", "false")
			w.Header().Set("Builder-Version", "2")
			w.WriteHeader(http.StatusOK)
		},
	})}

	got, err := adapter.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got.APIVersion != "1.43" {
		t.Errorf("APIVersion = %q, want 1.43", got.APIVersion)
	}
	if got.OSType != "linux" {
		t.Errorf("OSType = %q, want linux", got.OSType)
	}
}

func TestSystemServiceAdapter_Version_MapsFields(t *testing.T) {
	t.Parallel()

	adapter := &SystemServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/version": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"Version":       "27.1.1",
				"ApiVersion":    "1.46",
				"MinAPIVersion": "1.24",
				"Os":            "linux",
				"Arch":          "amd64",
				"Platform":      map[string]any{"Name": "Docker Engine - Community"},
				"Components": []map[string]any{
					{
						"Name":    "Engine",
						"Version": "27.1.1",
						"Details": map[string]string{
							"GitCommit":     "abc1234",
							"GoVersion":     "go1.22.5",
							"KernelVersion": "6.8.0",
							"BuildTime":     "2026-01-01T00:00:00.000000000+00:00",
						},
					},
				},
			})
		},
	})}

	got, err := adapter.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if got.Version != "27.1.1" || got.APIVersion != "1.46" {
		t.Errorf("Version/APIVersion = %q/%q, want 27.1.1/1.46", got.Version, got.APIVersion)
	}
	if got.Platform.Name != "Docker Engine - Community" {
		t.Errorf("Platform.Name = %q", got.Platform.Name)
	}
	// GitCommit and friends are lifted out of the Engine component's Details
	// map rather than read from a top-level field, which is the one piece of
	// logic in Version worth pinning.
	if got.GitCommit != "abc1234" {
		t.Errorf("GitCommit = %q, want abc1234 (lifted from the Engine component)", got.GitCommit)
	}
	if got.GoVersion != "go1.22.5" {
		t.Errorf("GoVersion = %q, want go1.22.5", got.GoVersion)
	}
	if len(got.Components) != 1 || got.Components[0].Name != "Engine" {
		t.Errorf("Components = %+v, want one Engine entry", got.Components)
	}
}

func TestSystemServiceAdapter_Info_MapsFields(t *testing.T) {
	t.Parallel()

	adapter := &SystemServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/info": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"ID":                "sys-id",
				"Name":              "test-daemon",
				"ServerVersion":     "27.1.1",
				"OperatingSystem":   "Debian",
				"OSType":            "linux",
				"Architecture":      "x86_64",
				"NCPU":              8,
				"MemTotal":          16000000000,
				"Containers":        3,
				"ContainersRunning": 1,
				"ContainersPaused":  0,
				"ContainersStopped": 2,
				"Images":            5,
				"Driver":            "overlay2",
			})
		},
	})}

	got, err := adapter.Info(context.Background())
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if got.Name != "test-daemon" {
		t.Errorf("Name = %q, want test-daemon", got.Name)
	}
	if got.Containers != 3 || got.ContainersRunning != 1 || got.ContainersStopped != 2 {
		t.Errorf("container counts = %d/%d/%d, want 3/1/2",
			got.Containers, got.ContainersRunning, got.ContainersStopped)
	}
	if got.NCPU != 8 {
		t.Errorf("NCPU = %d, want 8", got.NCPU)
	}
}

func TestImageServiceAdapter_List_ConvertsSummaries(t *testing.T) {
	t.Parallel()

	adapter := &ImageServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/images/json": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{
				{"Id": "sha256:aaa", "RepoTags": []string{"alpine:3.20"}, "Size": 1234},
				{"Id": "sha256:bbb", "RepoTags": []string{"busybox:latest"}, "Size": 5678},
			})
		},
	})}

	got, err := adapter.List(context.Background(), domain.ImageListOptions{All: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d images, want 2", len(got))
	}
	if got[0].ID != "sha256:aaa" || got[1].ID != "sha256:bbb" {
		t.Errorf("List returned ids %q, %q; want sha256:aaa, sha256:bbb", got[0].ID, got[1].ID)
	}
}

// TestImageServiceAdapter_Exists covers both branches of the not-found
// translation: Exists must report absence as (false, nil) and never as an
// error, because callers use it to decide whether to pull.
func TestImageServiceAdapter_Exists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		status  int
		want    bool
		wantErr bool
	}{
		{name: "present", status: http.StatusOK, want: true},
		{name: "absent", status: http.StatusNotFound, want: false},
		{name: "daemon error", status: http.StatusInternalServerError, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			adapter := &ImageServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
				"/images/": func(w http.ResponseWriter, _ *http.Request) {
					if tc.status != http.StatusOK {
						w.WriteHeader(tc.status)
						writeJSON(t, w, map[string]any{"message": "nope"})
						return
					}
					writeJSON(t, w, map[string]any{"Id": "sha256:aaa", "RepoTags": []string{"alpine:3.20"}})
				},
			})}

			got, err := adapter.Exists(context.Background(), "alpine:3.20")
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected an error for a failing daemon, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("Exists: %v", err)
			}
			if got != tc.want {
				t.Errorf("Exists = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestImageServiceAdapter_Tag_SendsRepoAndTag(t *testing.T) {
	t.Parallel()

	var gotQuery, gotPath string
	adapter := &ImageServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/tag": func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			gotQuery = r.URL.RawQuery
			w.WriteHeader(http.StatusCreated)
		},
	})}

	if err := adapter.Tag(context.Background(), "alpine:3.20", "myrepo/alpine:pinned"); err != nil {
		t.Fatalf("Tag: %v", err)
	}
	if gotPath == "" {
		t.Fatal("Tag never reached the daemon")
	}
	// The SDK splits the target into repo and tag query parameters; assert the
	// tag survived rather than the exact encoding of the repo path.
	if want := "tag=pinned"; !strings.Contains(gotQuery, want) {
		t.Errorf("tag request query = %q, want it to contain %q", gotQuery, want)
	}
}

func TestNetworkServiceAdapter_List_ConvertsNetworks(t *testing.T) {
	t.Parallel()

	adapter := &NetworkServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/networks": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{
				{"Id": "net-1", "Name": "bridge", "Driver": "bridge", "Scope": "local"},
				{"Id": "net-2", "Name": "host", "Driver": "host", "Scope": "local"},
			})
		},
	})}

	got, err := adapter.List(context.Background(), domain.NetworkListOptions{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List returned %d networks, want 2", len(got))
	}
	if got[0].Name != "bridge" || got[1].Name != "host" {
		t.Errorf("List returned %q, %q; want bridge, host", got[0].Name, got[1].Name)
	}
}

func TestNetworkServiceAdapter_Create_ReturnsID(t *testing.T) {
	t.Parallel()

	adapter := &NetworkServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/networks/create": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"Id": "new-net-id", "Warning": ""})
		},
	})}

	id, err := adapter.Create(context.Background(), "ofelia-net", ports.NetworkCreateOptions{Driver: "bridge"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != "new-net-id" {
		t.Errorf("Create returned %q, want new-net-id", id)
	}
}

func TestNetworkServiceAdapter_ConnectDisconnectRemove(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		route  string
		invoke func(*NetworkServiceAdapter) error
	}{
		{
			name:  "Connect",
			route: "/connect",
			invoke: func(a *NetworkServiceAdapter) error {
				return a.Connect(context.Background(), "net", "container", nil)
			},
		},
		{
			name:  "Disconnect",
			route: "/disconnect",
			invoke: func(a *NetworkServiceAdapter) error {
				return a.Disconnect(context.Background(), "net", "container", true)
			},
		},
		{
			name:  "Remove",
			route: "/networks",
			invoke: func(a *NetworkServiceAdapter) error {
				return a.Remove(context.Background(), "net")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			reached := false
			adapter := &NetworkServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
				tc.route: func(w http.ResponseWriter, _ *http.Request) {
					reached = true
					w.WriteHeader(http.StatusOK)
				},
			})}

			if err := tc.invoke(adapter); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !reached {
				t.Errorf("%s never reached the daemon", tc.name)
			}
		})
	}
}

// TestSystemServiceAdapter_DiskUsage_ConvertsInventory covers the conversion
// loops, which are the substance of DiskUsage: the daemon reports images,
// containers and volumes in one payload and each list has to arrive on the
// matching domain field rather than being silently dropped.
func TestSystemServiceAdapter_DiskUsage_ConvertsInventory(t *testing.T) {
	t.Parallel()

	adapter := &SystemServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/system/df": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"LayersSize": 4096,
				"Images": []map[string]any{
					{"Id": "sha256:img1", "RepoTags": []string{"alpine:3.20"}, "Size": 4096, "Containers": 1},
				},
				"Containers": []map[string]any{
					{"Id": "cnt1", "Names": []string{"/one"}, "Image": "alpine:3.20"},
				},
				"Volumes": []map[string]any{
					{"Name": "vol1", "Driver": "local", "Mountpoint": "/var/lib/docker/volumes/vol1"},
				},
			})
		},
	})}

	got, err := adapter.DiskUsage(context.Background())
	if err != nil {
		t.Fatalf("DiskUsage: %v", err)
	}
	if len(got.Images) != 1 || got.Images[0].ID != "sha256:img1" {
		t.Errorf("Images = %+v, want one entry with id sha256:img1", got.Images)
	}
	if len(got.Containers) != 1 || got.Containers[0].ID != "cnt1" {
		t.Errorf("Containers = %+v, want one entry with id cnt1", got.Containers)
	}
}

// TestImageServiceAdapter_Inspect_ReturnsImage and the Remove case below cover
// the two image operations a job actually performs around a pull.
func TestImageServiceAdapter_Inspect_ReturnsImage(t *testing.T) {
	t.Parallel()

	adapter := &ImageServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/images/": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"Id":       "sha256:inspected",
				"RepoTags": []string{"alpine:3.20"},
				"Size":     4096,
			})
		},
	})}

	got, err := adapter.Inspect(context.Background(), "alpine:3.20")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.ID != "sha256:inspected" {
		t.Errorf("Inspect returned id %q, want sha256:inspected", got.ID)
	}
}

func TestImageServiceAdapter_Remove_Succeeds(t *testing.T) {
	t.Parallel()

	adapter := &ImageServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/images/": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []map[string]any{{"Deleted": "sha256:gone"}})
		},
	})}

	if err := adapter.Remove(context.Background(), "sha256:gone", true, true); err != nil {
		t.Fatalf("Remove: %v", err)
	}
}

func TestNetworkServiceAdapter_Inspect_ReturnsNetwork(t *testing.T) {
	t.Parallel()

	adapter := &NetworkServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/networks/": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"Id":     "net-inspected",
				"Name":   "ofelia-net",
				"Driver": "bridge",
				"Scope":  "local",
			})
		},
	})}

	got, err := adapter.Inspect(context.Background(), "net-inspected")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.Name != "ofelia-net" {
		t.Errorf("Inspect returned name %q, want ofelia-net", got.Name)
	}
}

// TestImageServiceAdapter_Inspect_NilConfig pins the guard added alongside
// these tests: the SDK's image-inspect response carries Config as a pointer,
// and a daemon that omits it used to panic here rather than return an image.
// Writing the success-path test above is what surfaced it.
func TestImageServiceAdapter_Inspect_NilConfig(t *testing.T) {
	t.Parallel()

	defer failOnPanic(t, "Inspect on a response without Config")()

	adapter := &ImageServiceAdapter{client: stubSDK(t, map[string]http.HandlerFunc{
		"/images/": func(w http.ResponseWriter, _ *http.Request) {
			// No "Config" key at all — the shape a proxy or a
			// Docker-compatible API may return.
			writeJSON(t, w, map[string]any{
				"Id":       "sha256:noconfig",
				"RepoTags": []string{"alpine:3.20"},
			})
		},
	})}

	got, err := adapter.Inspect(context.Background(), "alpine:3.20")
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if got.ID != "sha256:noconfig" {
		t.Errorf("Inspect returned id %q, want sha256:noconfig", got.ID)
	}
	if got.Labels != nil {
		t.Errorf("Labels = %v, want nil when the daemon sent no Config", got.Labels)
	}
}
