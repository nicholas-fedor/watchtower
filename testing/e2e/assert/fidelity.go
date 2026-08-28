package assert

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
)

// InspectSnapshot is the subset of docker inspect compared across a recreate.
type InspectSnapshot struct {
	Name            string              `json:"name"`
	ImageRef        string              `json:"image_ref"`
	ImageID         string              `json:"image_id"`
	Env             []string            `json:"env"`
	Labels          map[string]string   `json:"labels"`
	User            string              `json:"user"`
	WorkingDir      string              `json:"working_dir"`
	Entrypoint      []string            `json:"entrypoint"`
	Cmd             []string            `json:"cmd"`
	Hostname        string              `json:"hostname"`
	Domainname      string              `json:"domainname"`
	ExtraHosts      []string            `json:"extra_hosts"`
	RestartPolicy   string              `json:"restart_policy"`
	Privileged      bool                `json:"privileged"`
	CapAdd          []string            `json:"cap_add"`
	CapDrop         []string            `json:"cap_drop"`
	NetworkMode     string              `json:"network_mode"`
	Networks        []string            `json:"networks"`
	Aliases         map[string][]string `json:"aliases"`
	Binds           []string            `json:"binds"`
	Tmpfs           map[string]string   `json:"tmpfs"`
	HealthcheckTest []string            `json:"healthcheck_test"`
	Memory          int64               `json:"memory"`
	NanoCPUs        int64               `json:"nano_cpus"`
	ReadonlyRootfs  bool                `json:"readonly_rootfs"`
	Tty             bool                `json:"tty"`
	ConfigMAC       string              `json:"config_mac"`
}

// FidelityError lists inspect fields that vanished or changed illegally across a recreate.
type FidelityError struct {
	// Mismatches are human-readable field diffs.
	Mismatches []string
}

// Error implements error.
func (e *FidelityError) Error() string {
	return "recreate fidelity: " + strings.Join(e.Mismatches, "; ")
}

// DiffFidelity compares inspect-before and inspect-after for a successful update.
//
// Image ID is allowed to change. Image-baked ENV (TAG, REV) may change with it.
// Image reference (tag), networks, labels,
// ports, volumes, restart policy, and healthcheck must not vanish.
//
// Parameters:
//   - before: Inspect snapshot taken before Watchtower ran.
//   - after: Inspect snapshot taken after the session.
//
// Returns:
//   - error: *FidelityError when operator intent was dropped.
func DiffFidelity(before, after InspectSnapshot) error {
	mismatches := make([]string, 0)

	if before.Name != after.Name {
		mismatches = append(mismatches, fmt.Sprintf("name %q -> %q", before.Name, after.Name))
	}

	if before.ImageRef != "" && after.ImageRef != "" && stripDigest(before.ImageRef) != stripDigest(after.ImageRef) {
		mismatches = append(mismatches, fmt.Sprintf("image ref %q -> %q", before.ImageRef, after.ImageRef))
	}

	checkMap("labels", before.Labels, after.Labels, &mismatches)

	if before.User != after.User {
		mismatches = append(mismatches, fmt.Sprintf("user %q -> %q", before.User, after.User))
	}

	if before.WorkingDir != after.WorkingDir {
		mismatches = append(mismatches, fmt.Sprintf("workdir %q -> %q", before.WorkingDir, after.WorkingDir))
	}

	checkSlice("entrypoint", before.Entrypoint, after.Entrypoint, &mismatches)
	checkSlice("cmd", before.Cmd, after.Cmd, &mismatches)

	if before.Hostname != after.Hostname {
		mismatches = append(mismatches, fmt.Sprintf("hostname %q -> %q", before.Hostname, after.Hostname))
	}

	if before.RestartPolicy != after.RestartPolicy {
		mismatches = append(mismatches, fmt.Sprintf("restart policy %q -> %q", before.RestartPolicy, after.RestartPolicy))
	}

	if before.NetworkMode != after.NetworkMode {
		mismatches = append(mismatches, fmt.Sprintf("network mode %q -> %q", before.NetworkMode, after.NetworkMode))
	}

	if missingNetwork(before.Networks, after.Networks) {
		mismatches = append(mismatches, fmt.Sprintf("networks %v -> %v", before.Networks, after.Networks))
	}

	checkSlice("binds", before.Binds, after.Binds, &mismatches)
	checkSlice("healthcheck", before.HealthcheckTest, after.HealthcheckTest, &mismatches)

	if before.Memory != 0 && after.Memory != before.Memory {
		mismatches = append(mismatches, fmt.Sprintf("memory %d -> %d", before.Memory, after.Memory))
	}

	if len(mismatches) == 0 {
		return nil
	}

	return &FidelityError{Mismatches: mismatches}
}

// stripDigest drops an @sha256 suffix so tag refs can be compared across recreates.
//
// Parameters:
//   - ref: Image reference.
//
// Returns:
//   - string: Reference without digest.
func stripDigest(ref string) string {
	if before, _, ok := strings.Cut(ref, "@"); ok {
		return before
	}

	return ref
}

// checkSlice records a mismatch when a non-empty before slice is not equal to after.
//
// Parameters:
//   - field: Inspect field name for the error text.
//   - before: Snapshot before the update.
//   - after: Snapshot after the update.
//   - mismatches: Accumulator.
func checkSlice(field string, before, after []string, mismatches *[]string) {
	if len(before) == 0 {
		return
	}

	if !reflect.DeepEqual(before, after) {
		*mismatches = append(*mismatches, fmt.Sprintf("%s %v -> %v", field, before, after))
	}
}

// checkMap records a mismatch when a before map key is missing or changed after.
//
// Parameters:
//   - field: Inspect field name for the error text.
//   - before: Snapshot before the update.
//   - after: Snapshot after the update.
//   - mismatches: Accumulator.
func checkMap(field string, before, after map[string]string, mismatches *[]string) {
	if len(before) == 0 {
		return
	}

	for key, value := range before {
		if after[key] != value {
			*mismatches = append(*mismatches, fmt.Sprintf("%s[%s] %q -> %q", field, key, value, after[key]))
		}
	}
}

// missingNetwork reports whether any before network name is absent after recreate.
//
// Parameters:
//   - before: Networks before the update.
//   - after: Networks after the update.
//
// Returns:
//   - bool: True when a network vanished.
func missingNetwork(before, after []string) bool {
	if len(before) == 0 {
		return false
	}

	have := make(map[string]struct{}, len(after))
	for _, name := range after {
		have[name] = struct{}{}
	}

	for _, name := range before {
		if _, ok := have[name]; !ok {
			return true
		}
	}

	return false
}

// ParseInspect decodes a docker inspect JSON blob into InspectSnapshot.
//
// Parameters:
//   - raw: docker inspect JSON for one container.
//
// Returns:
//   - InspectSnapshot: Compared fields.
//   - error: JSON error.
func ParseInspect(raw []byte) (InspectSnapshot, error) {
	var payload inspectJSON

	err := json.Unmarshal(raw, &payload)
	if err != nil {
		return InspectSnapshot{}, fmt.Errorf("parse inspect: %w", err)
	}

	networks := make([]string, 0, len(payload.NetworkSettings.Networks))

	aliases := make(map[string][]string, len(payload.NetworkSettings.Networks))
	for name, net := range payload.NetworkSettings.Networks {
		networks = append(networks, name)
		aliases[name] = net.Aliases
	}

	return InspectSnapshot{
		Name:            strings.TrimPrefix(payload.Name, "/"),
		ImageRef:        payload.Config.Image,
		ImageID:         payload.Image,
		Env:             payload.Config.Env,
		Labels:          payload.Config.Labels,
		User:            payload.Config.User,
		WorkingDir:      payload.Config.WorkingDir,
		Entrypoint:      payload.Config.Entrypoint,
		Cmd:             payload.Config.Cmd,
		Hostname:        payload.Config.Hostname,
		Domainname:      payload.Config.Domainname,
		RestartPolicy:   payload.HostConfig.RestartPolicy.Name,
		Privileged:      payload.HostConfig.Privileged,
		CapAdd:          payload.HostConfig.CapAdd,
		CapDrop:         payload.HostConfig.CapDrop,
		NetworkMode:     payload.HostConfig.NetworkMode,
		Networks:        networks,
		Aliases:         aliases,
		Binds:           payload.HostConfig.Binds,
		Tmpfs:           payload.HostConfig.Tmpfs,
		HealthcheckTest: healthTest(payload.Config.Healthcheck),
		Memory:          payload.HostConfig.Memory,
		NanoCPUs:        payload.HostConfig.NanoCPUs,
		ReadonlyRootfs:  payload.HostConfig.ReadonlyRootfs,
		Tty:             payload.Config.Tty,
		ConfigMAC:       payload.Config.MacAddress,
	}, nil
}

// inspectJSON is the docker inspect JSON shape ParseInspect reads.
type inspectJSON struct {
	Name            string        `json:"Name"`
	Image           string        `json:"Image"`
	Config          inspectConfig `json:"Config"`
	HostConfig      inspectHost   `json:"HostConfig"`
	NetworkSettings inspectNets   `json:"NetworkSettings"`
}

// inspectConfig is the Config object from docker inspect.
type inspectConfig struct {
	Image       string            `json:"Image"`
	Env         []string          `json:"Env"`
	Labels      map[string]string `json:"Labels"`
	User        string            `json:"User"`
	WorkingDir  string            `json:"WorkingDir"`
	Entrypoint  []string          `json:"Entrypoint"`
	Cmd         []string          `json:"Cmd"`
	Hostname    string            `json:"Hostname"`
	Domainname  string            `json:"Domainname"`
	Tty         bool              `json:"Tty"`
	MacAddress  string            `json:"MacAddress"`
	Healthcheck *inspectHealth    `json:"Healthcheck"`
}

// inspectHealth is the Healthcheck object from docker inspect.
type inspectHealth struct {
	Test []string `json:"Test"`
}

// inspectHost is the HostConfig object from docker inspect.
type inspectHost struct {
	RestartPolicy struct {
		Name string `json:"Name"`
	} `json:"RestartPolicy"`
	Privileged     bool              `json:"Privileged"`
	CapAdd         []string          `json:"CapAdd"`
	CapDrop        []string          `json:"CapDrop"`
	NetworkMode    string            `json:"NetworkMode"`
	Binds          []string          `json:"Binds"`
	Tmpfs          map[string]string `json:"Tmpfs"`
	Memory         int64             `json:"Memory"`
	NanoCPUs       int64             `json:"NanoCpus"`
	ReadonlyRootfs bool              `json:"ReadonlyRootfs"`
}

// inspectNets is the NetworkSettings object from docker inspect.
type inspectNets struct {
	// Networks is the per-network endpoint map.
	Networks map[string]inspectNet `json:"Networks"`
}

// inspectNet is one network endpoint from docker inspect.
type inspectNet struct {
	// Aliases are DNS aliases on that network.
	Aliases []string `json:"Aliases"`
}

// healthTest returns the HEALTHCHECK Test slice, or nil.
//
// Parameters:
//   - health: Inspect Healthcheck block.
//
// Returns:
//   - []string: Test command, or nil.
func healthTest(health *inspectHealth) []string {
	if health == nil {
		return nil
	}

	return health.Test
}
