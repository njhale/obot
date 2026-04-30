package apiclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/obot-platform/obot/apiclient/types"
)

// SubmitDeviceScan posts a device scan payload to the server. The
// returned DeviceScan carries the server-assigned ID and ReceivedAt.
//
// The CLI is responsible for filling in the wire-shape envelope
// (DeviceID, Hostname, OS, etc.) and the four child slices. SubmittedBy
// and ID are server-assigned and ignored if the caller pre-populates
// them.
func (c *Client) SubmitDeviceScan(ctx context.Context, scan types.DeviceScan) (*types.DeviceScan, error) {
	_, resp, err := c.postJSON(ctx, "/devices/scans", scan)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	out, err := toObject(resp, &types.DeviceScan{})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetDeviceScan fetches a single scan by ID, including all of its
// child slices (MCP servers, skills, plugins, files).
func (c *Client) GetDeviceScan(ctx context.Context, id uint) (*types.DeviceScan, error) {
	_, resp, err := c.doRequest(ctx, http.MethodGet, fmt.Sprintf("/devices/scans/%d", id), nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return toObject(resp, &types.DeviceScan{})
}

// ListDeviceScans returns scan envelopes (no children). Use
// GetDeviceScan(id) to fetch a specific scan with its children.
func (c *Client) ListDeviceScans(ctx context.Context) (*types.DeviceScanList, error) {
	_, resp, err := c.doRequest(ctx, http.MethodGet, "/devices/scans", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return toObject(resp, &types.DeviceScanList{})
}
