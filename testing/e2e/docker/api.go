package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moby/moby/client"
)

// watchtowerAPIPort is Watchtower's HTTP API port inside DinD.
const watchtowerAPIPort = "8080"

// GetContainersDetails fetches GET /v1/containers/details from Watchtower inside DinD.
//
// Parameters:
//   - ctx: Cancellation.
//   - daemon: Inner DinD worker.
//   - watchtowerID: Watchtower container ID.
//   - token: Bearer token.
//
// Returns:
//   - string: Response body.
//   - error: Inspect, wget, or empty body.
func GetContainersDetails(ctx context.Context, daemon *Daemon, watchtowerID, token string) (string, error) {
	view, err := daemon.Client().ContainerInspect(ctx, watchtowerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("inspect watchtower: %w", err)
	}

	ip := containerIP(view)
	if ip == "" {
		return "", errWatchtowerNoIP
	}

	url := "http://" + ip + ":" + watchtowerAPIPort + "/v1/containers/details"

	out, execErr := daemon.Exec(ctx, []string{
		"wget", "-q", "-O", "-",
		"--header=Authorization: Bearer " + token,
		url,
	})
	if execErr != nil {
		return "", fmt.Errorf("wget details: %w", execErr)
	}

	return out, nil
}

func containerIP(view client.ContainerInspectResult) string {
	raw, err := json.Marshal(view)
	if err != nil {
		return ""
	}

	var payload struct {
		Container struct {
			NetworkSettings struct {
				IPAddress string `json:"IPAddress"`
				Networks  map[string]struct {
					IPAddress string `json:"IPAddress"`
				} `json:"Networks"`
			} `json:"NetworkSettings"`
		} `json:"Container"`
		NetworkSettings struct {
			IPAddress string `json:"IPAddress"`
			Networks  map[string]struct {
				IPAddress string `json:"IPAddress"`
			} `json:"Networks"`
		} `json:"NetworkSettings"`
	}

	unmarshalErr := json.Unmarshal(raw, &payload)
	if unmarshalErr != nil {
		return ""
	}

	if payload.NetworkSettings.IPAddress != "" {
		return payload.NetworkSettings.IPAddress
	}

	for _, net := range payload.NetworkSettings.Networks {
		if net.IPAddress != "" {
			return net.IPAddress
		}
	}

	if payload.Container.NetworkSettings.IPAddress != "" {
		return payload.Container.NetworkSettings.IPAddress
	}

	for _, net := range payload.Container.NetworkSettings.Networks {
		if net.IPAddress != "" {
			return net.IPAddress
		}
	}

	return ""
}

// DetailsEnabledTrue reports whether the named container is enabled in details JSON.
//
// Parameters:
//   - body: /v1/containers/details body.
//   - name: Container name fragment.
//
// Returns:
//   - error: When enabled is missing or false.
func DetailsEnabledTrue(body, name string) error {
	if !strings.Contains(body, name) {
		return fmt.Errorf("%w: %s not in details", errDetailsMissing, name)
	}

	if strings.Contains(body, `"enabled":true`) || strings.Contains(body, `"enabled": true`) {
		return nil
	}

	return errDetailsNotEnabled
}
