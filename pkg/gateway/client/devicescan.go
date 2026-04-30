package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/obot-platform/obot/pkg/gateway/types"
	"gorm.io/gorm"
)

// InsertDeviceScan persists a device scan envelope and all its children
// in a single GORM cascading insert. Each call creates a fresh row —
// duplicate submissions are not deduped at this layer.
func (c *Client) InsertDeviceScan(ctx context.Context, scan *types.DeviceScan) error {
	if scan == nil {
		return errors.New("nil device scan")
	}
	if err := c.db.WithContext(ctx).Create(scan).Error; err != nil {
		return fmt.Errorf("failed to insert device scan: %w", err)
	}
	return nil
}

// GetDeviceScan loads a single scan with all children preloaded.
func (c *Client) GetDeviceScan(ctx context.Context, id uint) (*types.DeviceScan, error) {
	var s types.DeviceScan
	if err := c.db.WithContext(ctx).
		Preload("MCPServers").
		Preload("Skills").
		Preload("Plugins").
		Preload("Files").
		First(&s, id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

// DeviceScanListOptions filters the scan-envelope list endpoint.
// SubmittedBy and DeviceID are multi-value; either narrows the result.
type DeviceScanListOptions struct {
	SubmittedBy   []string
	DeviceID      []string
	Limit         int
	Offset        int
	GroupByDevice bool
}

// ListDeviceScans returns scan envelopes ordered newest first.
// MCP servers, skills, and plugins are preloaded; files are not —
// DeviceScanFile.Content can be large and isn't needed for the list.
func (c *Client) ListDeviceScans(ctx context.Context, opts DeviceScanListOptions) ([]types.DeviceScan, int64, error) {
	db := c.db.WithContext(ctx).Model(&types.DeviceScan{})
	db = applyDeviceScanListFilters(db, opts)

	if opts.GroupByDevice {
		sub := applyDeviceScanListFilters(
			c.db.WithContext(ctx).Model(&types.DeviceScan{}).Select("MAX(id)"),
			opts,
		).Group("device_id")
		db = db.Where("id IN (?)", sub)
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if opts.Limit > 0 {
		db = db.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		db = db.Offset(opts.Offset)
	}

	var scans []types.DeviceScan
	if err := db.Order("created_at DESC").
		Preload("MCPServers").
		Preload("Skills").
		Preload("Plugins").
		Find(&scans).Error; err != nil {
		return nil, 0, err
	}
	return scans, total, nil
}

func applyDeviceScanListFilters(db *gorm.DB, opts DeviceScanListOptions) *gorm.DB {
	if len(opts.SubmittedBy) > 0 {
		db = db.Where("submitted_by IN (?)", opts.SubmittedBy)
	}
	if len(opts.DeviceID) > 0 {
		db = db.Where("device_id IN (?)", opts.DeviceID)
	}
	return db
}

// AggregateMCPServerOptions filters and orders the fleet-wide MCP
// aggregation. The time window applies to the parent device_scans:
// only scans whose ScannedAt falls inside [StartTime, EndTime) are
// candidates for "latest per device" selection, and rows from devices
// whose newest in-window scan is older than StartTime drop out
// entirely. Zero-valued bounds are treated as "unbounded".
type AggregateMCPServerOptions struct {
	StartTime time.Time
	EndTime   time.Time

	Name       string
	Command    string
	URL        string
	Transports []string
	Clients    []string

	SortBy    string // name | device_count | user_count | client_count | first_seen | last_seen
	SortOrder string // asc | desc

	Limit  int
	Offset int
}

var aggregateSortColumns = map[string]string{
	"name":         "name",
	"device_count": "device_count",
	"user_count":   "user_count",
	"client_count": "client_count",
	"first_seen":   "first_seen",
	"last_seen":    "last_seen",
}

// AggregateMCPServers returns one row per ConfigHash observed in the
// latest scan of any device within the requested window. Aggregates
// are computed across all observations of that hash within the window.
// The default sort is device_count DESC, with config_hash ASC as a
// stable tiebreaker.
func (c *Client) AggregateMCPServers(ctx context.Context, opts AggregateMCPServerOptions) ([]types.AggregatedMCPServer, int64, error) {
	base, err := c.aggregateBase(ctx, opts)
	if err != nil {
		return nil, 0, err
	}

	// Total count = number of distinct config_hash groups after WHERE.
	var total int64
	if err := base.Session(&gorm.Session{}).
		Distinct("m.config_hash").
		Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count aggregated mcp servers: %w", err)
	}

	sortCol := aggregateSortColumns[opts.SortBy]
	if sortCol == "" {
		sortCol = "device_count"
	}
	sortDir := "DESC"
	if strings.EqualFold(opts.SortOrder, "asc") {
		sortDir = "ASC"
	}

	q := base.Session(&gorm.Session{}).
		Select(`m.config_hash AS config_hash,
			MAX(m.name) AS name,
			MAX(m.transport) AS transport,
			MAX(m.command) AS command,
			MAX(m.url) AS url,
			COUNT(DISTINCT s.device_id) AS device_count,
			COUNT(DISTINCT s.submitted_by) AS user_count,
			COUNT(DISTINCT m.client) AS client_count,
			COUNT(DISTINCT m.scope) AS scope_count,
			COUNT(*) AS observation_count,
			MIN(s.scanned_at) AS first_seen,
			MAX(s.scanned_at) AS last_seen`).
		Group("m.config_hash").
		Order(fmt.Sprintf("%s %s, m.config_hash ASC", sortCol, sortDir))

	if opts.Limit > 0 {
		q = q.Limit(opts.Limit)
	}
	if opts.Offset > 0 {
		q = q.Offset(opts.Offset)
	}

	var rows []types.AggregatedMCPServer
	if err := q.Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to aggregate mcp servers: %w", err)
	}
	return rows, total, nil
}

// aggregateBase returns a *gorm.DB chained with the latest-scan-per-device
// subquery and any caller-supplied row filters. The returned chain has
// neither SELECT nor GROUP BY applied yet, so it can be reused for both
// COUNT and the aggregate SELECT.
func (c *Client) aggregateBase(ctx context.Context, opts AggregateMCPServerOptions) (*gorm.DB, error) {
	db := c.db.WithContext(ctx)

	latest := db.Model(&types.DeviceScan{}).Select("MAX(id)")
	if !opts.StartTime.IsZero() {
		latest = latest.Where("scanned_at >= ?", opts.StartTime)
	}
	if !opts.EndTime.IsZero() {
		latest = latest.Where("scanned_at < ?", opts.EndTime)
	}
	latest = latest.Group("device_id")

	base := db.Table("device_scan_mcp_servers AS m").
		Joins("JOIN device_scans AS s ON s.id = m.device_scan_id").
		Where("s.id IN (?)", latest)

	if len(opts.Transports) > 0 {
		base = base.Where("m.transport IN ?", opts.Transports)
	}
	if len(opts.Clients) > 0 {
		base = base.Where("m.client IN ?", opts.Clients)
	}

	like := "LIKE"
	if db.Name() == "postgres" {
		like = "ILIKE"
	}
	if opts.Name != "" {
		base = base.Where(fmt.Sprintf("m.name %s ?", like), "%"+opts.Name+"%")
	}
	if opts.Command != "" {
		base = base.Where(fmt.Sprintf("m.command %s ?", like), "%"+opts.Command+"%")
	}
	if opts.URL != "" {
		base = base.Where(fmt.Sprintf("m.url %s ?", like), "%"+opts.URL+"%")
	}

	return base, nil
}

// GetAggregatedMCPServer returns a single aggregated row keyed by
// config_hash. The aggregation is unbounded (all-time, all latest
// scans per device), per the detail-page semantics. Args / EnvKeys /
// HeaderKeys are pulled from the underlying observations: Args from
// any single canonical row (constant within a hash); EnvKeys and
// HeaderKeys are unioned across all observations.
func (c *Client) GetAggregatedMCPServer(ctx context.Context, configHash string) (*types.AggregatedMCPServerDetail, error) {
	if configHash == "" {
		return nil, errors.New("empty config hash")
	}
	db := c.db.WithContext(ctx)

	latest := db.Model(&types.DeviceScan{}).Select("MAX(id)").Group("device_id")

	var agg types.AggregatedMCPServer
	row := db.Table("device_scan_mcp_servers AS m").
		Joins("JOIN device_scans AS s ON s.id = m.device_scan_id").
		Where("s.id IN (?)", latest).
		Where("m.config_hash = ?", configHash).
		Select(`m.config_hash AS config_hash,
			MAX(m.name) AS name,
			MAX(m.transport) AS transport,
			MAX(m.command) AS command,
			MAX(m.url) AS url,
			COUNT(DISTINCT s.device_id) AS device_count,
			COUNT(DISTINCT s.submitted_by) AS user_count,
			COUNT(DISTINCT m.client) AS client_count,
			COUNT(DISTINCT m.scope) AS scope_count,
			COUNT(*) AS observation_count,
			MIN(s.scanned_at) AS first_seen,
			MAX(s.scanned_at) AS last_seen`).
		Group("m.config_hash").
		Scan(&agg)
	if row.Error != nil {
		return nil, fmt.Errorf("failed to load aggregated mcp server: %w", row.Error)
	}
	if row.RowsAffected == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	// Pull Args/EnvKeys/HeaderKeys from the actual rows. Args is constant
	// within a hash group; EnvKeys / HeaderKeys are unioned in Go.
	var canonical []types.DeviceScanMCPServer
	if err := db.
		Where("config_hash = ?", configHash).
		Where("device_scan_id IN (?)", latest).
		Find(&canonical).Error; err != nil {
		return nil, fmt.Errorf("failed to load mcp server details: %w", err)
	}

	out := &types.AggregatedMCPServerDetail{AggregatedMCPServer: agg}
	if len(canonical) > 0 {
		out.Args = []string(canonical[0].Args)
		envSeen := map[string]struct{}{}
		hdrSeen := map[string]struct{}{}
		for _, r := range canonical {
			for _, k := range r.EnvKeys {
				if _, ok := envSeen[k]; ok {
					continue
				}
				envSeen[k] = struct{}{}
				out.EnvKeys = append(out.EnvKeys, k)
			}
			for _, k := range r.HeaderKeys {
				if _, ok := hdrSeen[k]; ok {
					continue
				}
				hdrSeen[k] = struct{}{}
				out.HeaderKeys = append(out.HeaderKeys, k)
			}
		}
	}
	return out, nil
}

// ListMCPServerOccurrences returns one row per (device, observation)
// for the given config_hash, drawn from the all-time latest scan of
// every device. Sorted scanned_at DESC, paginated.
func (c *Client) ListMCPServerOccurrences(ctx context.Context, configHash string, limit, offset int) ([]types.MCPServerOccurrence, int64, error) {
	if configHash == "" {
		return nil, 0, errors.New("empty config hash")
	}
	db := c.db.WithContext(ctx)

	latest := db.Model(&types.DeviceScan{}).Select("MAX(id)").Group("device_id")

	base := db.Table("device_scan_mcp_servers AS m").
		Joins("JOIN device_scans AS s ON s.id = m.device_scan_id").
		Where("m.config_hash = ?", configHash).
		Where("s.id IN (?)", latest)

	var total int64
	if err := base.Session(&gorm.Session{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count occurrences: %w", err)
	}

	q := base.Session(&gorm.Session{}).
		Select(`m.device_scan_id AS device_scan_id,
			s.device_id AS device_id,
			m.client AS client,
			m.scope AS scope,
			s.scanned_at AS scanned_at,
			(SELECT COUNT(*) FROM device_scan_mcp_servers m2
			 WHERE m2.device_scan_id = m.device_scan_id AND m2.id < m.id) AS idx`).
		Order("s.scanned_at DESC, m.id ASC")

	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}

	var rows []types.MCPServerOccurrence
	if err := q.Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to list occurrences: %w", err)
	}
	return rows, total, nil
}

// ListMCPServerFilterOptions returns the distinct values of `field`
// observed in latest-scan-per-device rows within the time window.
// Used to populate the Transport / Client multi-select dropdowns on
// the list page.
func (c *Client) ListMCPServerFilterOptions(ctx context.Context, field string, opts AggregateMCPServerOptions) ([]string, error) {
	column, ok := map[string]string{
		"transport": "transport",
		"client":    "client",
		"scope":     "scope",
	}[field]
	if !ok {
		return nil, fmt.Errorf("invalid filter field: %s", field)
	}

	base, err := c.aggregateBase(ctx, opts)
	if err != nil {
		return nil, err
	}

	var values []string
	if err := base.Session(&gorm.Session{}).
		Select(fmt.Sprintf("DISTINCT m.%s", column)).
		Where(fmt.Sprintf("m.%s != ''", column)).
		Order(fmt.Sprintf("m.%s ASC", column)).
		Scan(&values).Error; err != nil {
		return nil, fmt.Errorf("failed to list filter options: %w", err)
	}
	return values, nil
}
