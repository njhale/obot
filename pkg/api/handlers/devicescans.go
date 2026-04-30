package handlers

import (
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	types "github.com/obot-platform/obot/apiclient/types"
	"github.com/obot-platform/obot/pkg/api"
	gateway "github.com/obot-platform/obot/pkg/gateway/client"
	gtypes "github.com/obot-platform/obot/pkg/gateway/types"
	"gorm.io/gorm"
)

// DeviceScansHandler serves the `obot scan` ingest + read API. Any
// authenticated user may submit a scan via POST and read their own
// scans via GET; admins / owners / auditors see every scan.
type DeviceScansHandler struct{}

func NewDeviceScansHandler() *DeviceScansHandler {
	return &DeviceScansHandler{}
}

// maxDeviceScanBodyBytes caps a single submission. Real payloads are
// well under this — `obot scan` itself is ~500 KiB on a busy laptop.
// 16 MiB is a generous ceiling that still protects against abuse.
const maxDeviceScanBodyBytes = 16 * 1024 * 1024

// Submit handles POST /api/devices/scans. The caller's identity is
// recorded as SubmittedBy; ID and ReceivedAt are server-assigned.
func (*DeviceScansHandler) Submit(req api.Context) error {
	var payload types.DeviceScan
	if err := readJSONBody(req, &payload, maxDeviceScanBodyBytes); err != nil {
		return err
	}

	scan := gtypes.DeviceScanFromWire(payload)
	scan.SubmittedBy = req.User.GetUID()

	if err := req.GatewayClient.InsertDeviceScan(req.Context(), &scan); err != nil {
		return err
	}

	return req.WriteCreated(gtypes.ConvertDeviceScan(scan))
}

// List handles GET /api/devices/scans. Non-privileged callers only see
// scans they themselves submitted; admins / owners / auditors see all
// scans, with optional submitted_by / device_id filters.
func (*DeviceScansHandler) List(req api.Context) error {
	opts := parseDeviceScanListOpts(req.URL.Query())
	if opts.Limit == 0 {
		opts.Limit = 100
	}
	if !privilegedDeviceScanReader(req) {
		// Hard pin to the caller, regardless of any submitted_by query
		// parameter they tried to pass.
		opts.SubmittedBy = []string{req.User.GetUID()}
	}

	scans, total, err := req.GatewayClient.ListDeviceScans(req.Context(), opts)
	if err != nil {
		return err
	}

	items := make([]types.DeviceScan, 0, len(scans))
	for _, s := range scans {
		items = append(items, gtypes.ConvertDeviceScan(s))
	}
	return req.Write(types.DeviceScanList{
		Items:  items,
		Total:  total,
		Limit:  opts.Limit,
		Offset: opts.Offset,
	})
}

// Get handles GET /api/devices/scans/{scan_id}. Returns the scan
// envelope plus all child rows (MCP servers, skills, plugins, files).
// A non-privileged caller asking for someone else's scan gets 404
// (never 403) so we don't leak that the scan exists.
func (*DeviceScansHandler) Get(req api.Context) error {
	id, err := parseDeviceScanID(req.PathValue("scan_id"))
	if err != nil {
		return err
	}
	scan, err := req.GatewayClient.GetDeviceScan(req.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NewErrNotFound("device scan %d not found", id)
		}
		return err
	}
	if !privilegedDeviceScanReader(req) && scan.SubmittedBy != req.User.GetUID() {
		return types.NewErrNotFound("device scan %d not found", id)
	}
	return req.Write(gtypes.ConvertDeviceScan(*scan))
}

func parseDeviceScanID(raw string) (uint, error) {
	if raw == "" {
		return 0, types.NewErrBadRequest("missing device scan id")
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, types.NewErrBadRequest("invalid device scan id: %v", err)
	}
	return uint(id), nil
}

// privilegedDeviceScanReader returns true if the caller is allowed to
// see scans they didn't submit themselves.
func privilegedDeviceScanReader(req api.Context) bool {
	return req.UserIsAdmin() || req.UserIsOwner() || req.UserIsAuditor()
}

func parseDeviceScanListOpts(query url.Values) gateway.DeviceScanListOptions {
	opts := gateway.DeviceScanListOptions{
		SubmittedBy:   parseMultiValueDeviceScan(query, "submitted_by"),
		DeviceID:      parseMultiValueDeviceScan(query, "device_id"),
		GroupByDevice: true,
	}
	if v := query.Get("group_by_device"); v != "" {
		if parsed, err := strconv.ParseBool(v); err == nil {
			opts.GroupByDevice = parsed
		}
	}
	if limitStr := query.Get("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			opts.Limit = l
		}
	}
	if offsetStr := query.Get("offset"); offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			opts.Offset = o
		}
	}
	return opts
}

// parseMultiValueDeviceScan accepts both repeated query params
// (?submitted_by=a&submitted_by=b) and comma-separated values
// (?submitted_by=a,b). Whitespace + empty entries are dropped.
func parseMultiValueDeviceScan(query url.Values, key string) []string {
	values := query[key]
	if len(values) == 0 {
		return nil
	}
	var out []string
	for _, v := range values {
		for part := range strings.SplitSeq(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ListAggregatedMCPServers handles GET /api/devices/mcp-servers. Returns
// one row per ConfigHash observed in the latest scan of any device
// within the requested time window. Admin / owner / auditor only — the
// page is fleet-wide by design and offers no useful view to others.
func (*DeviceScansHandler) ListAggregatedMCPServers(req api.Context) error {
	if !privilegedDeviceScanReader(req) {
		return types.NewErrForbidden("forbidden")
	}
	opts, err := parseAggregateMCPServerOpts(req.URL.Query())
	if err != nil {
		return err
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}

	rows, total, err := req.GatewayClient.AggregateMCPServers(req.Context(), opts)
	if err != nil {
		return err
	}
	items := make([]types.AggregatedDeviceMCPServer, 0, len(rows))
	for _, r := range rows {
		items = append(items, convertAggregatedMCPServer(r))
	}
	return req.Write(types.AggregatedDeviceMCPServerList{
		Items:  items,
		Total:  total,
		Limit:  opts.Limit,
		Offset: opts.Offset,
	})
}

// GetAggregatedMCPServer handles GET /api/devices/mcp-servers/{config_hash}.
// Returns the all-time aggregate for that hash plus Args / EnvKeys /
// HeaderKeys.
func (*DeviceScansHandler) GetAggregatedMCPServer(req api.Context) error {
	if !privilegedDeviceScanReader(req) {
		return types.NewErrForbidden("forbidden")
	}
	hash := req.PathValue("config_hash")
	if hash == "" {
		return types.NewErrBadRequest("missing config_hash")
	}
	detail, err := req.GatewayClient.GetAggregatedMCPServer(req.Context(), hash)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return types.NewErrNotFound("config %s not found", hash)
		}
		return err
	}
	return req.Write(convertAggregatedMCPServerDetail(*detail))
}

// ListMCPServerOccurrences handles
// GET /api/devices/mcp-servers/{config_hash}/occurrences. Returns one
// row per (device, observation) of the given hash from each device's
// all-time latest scan.
func (*DeviceScansHandler) ListMCPServerOccurrences(req api.Context) error {
	if !privilegedDeviceScanReader(req) {
		return types.NewErrForbidden("forbidden")
	}
	hash := req.PathValue("config_hash")
	if hash == "" {
		return types.NewErrBadRequest("missing config_hash")
	}
	q := req.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 {
			limit = l
		}
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			offset = o
		}
	}
	rows, total, err := req.GatewayClient.ListMCPServerOccurrences(req.Context(), hash, limit, offset)
	if err != nil {
		return err
	}
	items := make([]types.DeviceMCPServerOccurrence, 0, len(rows))
	for _, r := range rows {
		items = append(items, types.DeviceMCPServerOccurrence{
			DeviceScanID: r.DeviceScanID,
			DeviceID:     r.DeviceID,
			Client:       r.Client,
			Scope:        r.Scope,
			ScannedAt:    *types.NewTime(r.ScannedAt.Time),
			Index:        r.Index,
		})
	}
	return req.Write(types.DeviceMCPServerOccurrenceList{
		Items:  items,
		Total:  total,
		Limit:  limit,
		Offset: offset,
	})
}

// ListMCPServerFilterOptions handles
// GET /api/devices/mcp-server-filter-options/{field}. Returns distinct
// values of `field` (transport | client | scope) seen in the latest
// scans within the time window.
func (*DeviceScansHandler) ListMCPServerFilterOptions(req api.Context) error {
	if !privilegedDeviceScanReader(req) {
		return types.NewErrForbidden("forbidden")
	}
	field := req.PathValue("field")
	if field == "" {
		return types.NewErrBadRequest("missing field")
	}
	opts, err := parseAggregateMCPServerOpts(req.URL.Query())
	if err != nil {
		return err
	}
	values, err := req.GatewayClient.ListMCPServerFilterOptions(req.Context(), field, opts)
	if err != nil {
		return types.NewErrBadRequest("%v", err)
	}
	return req.Write(map[string]any{"options": values})
}

func parseAggregateMCPServerOpts(query url.Values) (gateway.AggregateMCPServerOptions, error) {
	opts := gateway.AggregateMCPServerOptions{
		Name:       strings.TrimSpace(query.Get("name")),
		Command:    strings.TrimSpace(query.Get("command")),
		URL:        strings.TrimSpace(query.Get("url")),
		Transports: parseMultiValueDeviceScan(query, "transport"),
		Clients:    parseMultiValueDeviceScan(query, "client"),
		SortBy:     query.Get("sort_by"),
		SortOrder:  query.Get("sort_order"),
	}
	if v := query.Get("start"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return opts, types.NewErrBadRequest("invalid start: %v", err)
		}
		opts.StartTime = t
	}
	if v := query.Get("end"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return opts, types.NewErrBadRequest("invalid end: %v", err)
		}
		opts.EndTime = t
	}
	if v := query.Get("limit"); v != "" {
		if l, err := strconv.Atoi(v); err == nil && l > 0 {
			opts.Limit = l
		}
	}
	if v := query.Get("offset"); v != "" {
		if o, err := strconv.Atoi(v); err == nil && o >= 0 {
			opts.Offset = o
		}
	}
	return opts, nil
}

func convertAggregatedMCPServer(r gtypes.AggregatedMCPServer) types.AggregatedDeviceMCPServer {
	return types.AggregatedDeviceMCPServer{
		ConfigHash:       r.ConfigHash,
		Name:             r.Name,
		Transport:        r.Transport,
		Command:          r.Command,
		URL:              r.URL,
		DeviceCount:      r.DeviceCount,
		UserCount:        r.UserCount,
		ClientCount:      r.ClientCount,
		ScopeCount:       r.ScopeCount,
		ObservationCount: r.ObservationCount,
		FirstSeen:        *types.NewTime(r.FirstSeen.Time),
		LastSeen:         *types.NewTime(r.LastSeen.Time),
	}
}

func convertAggregatedMCPServerDetail(d gtypes.AggregatedMCPServerDetail) types.AggregatedDeviceMCPServerDetail {
	return types.AggregatedDeviceMCPServerDetail{
		AggregatedDeviceMCPServer: convertAggregatedMCPServer(d.AggregatedMCPServer),
		Args:                      d.Args,
		EnvKeys:                   d.EnvKeys,
		HeaderKeys:                d.HeaderKeys,
	}
}

// readJSONBody reads + unmarshals a JSON request body, returning a
// 413 if the body exceeds maxBytes and a 400 if the JSON is malformed.
func readJSONBody(req api.Context, dst any, maxBytes int64) error {
	data, err := req.Body(api.BodyOptions{MaxBytes: maxBytes})
	if err != nil {
		return err
	}
	if len(data) == 0 {
		return types.NewErrBadRequest("empty request body")
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return types.NewErrBadRequest("invalid request body: %v", err)
	}
	return nil
}
