package mcp

import (
	"context"

	"ovh-nexus/server/internal/auth"
	"ovh-nexus/server/internal/config"
	"ovh-nexus/server/internal/logger"
	"ovh-nexus/server/internal/ovhclient"
	"ovh-nexus/server/internal/store"
)

// OVHClient abstracts authenticated OVHcloud API calls (satisfied by *ovh.Client).
type OVHClient interface {
	GetWithContext(ctx context.Context, path string, resType any) error
	PostWithContext(ctx context.Context, path string, reqBody, resType any) error
	PutWithContext(ctx context.Context, path string, reqBody, resType any) error
	DeleteWithContext(ctx context.Context, path string, resType any) error
}

// TOTPChecker checks whether panel 2FA (TOTP) is currently enabled.
// Satisfied by *auth.Service.
type TOTPChecker interface {
	IsTOTPEnabled(ctx context.Context) (bool, error)
}

// CatalogEntry summarizes a dedicated server model and its current availability.
type CatalogEntry struct {
	PlanCode               string   `json:"planCode"`
	ModelName              string   `json:"modelName"`
	Range                  string   `json:"range"`
	CPU                    string   `json:"cpu"`
	AvailableDatacenters   []string `json:"availableDatacenters"`
	MatchingConfigurations int      `json:"matchingConfigurations"`
}

// CatalogReader abstracts catalog resolution, global stock availability, and SKU changes.
type CatalogReader interface {
	QueryAvailability(ctx context.Context, planFilter, dcFilter string, onlyAvailable bool, limit int) ([]CatalogEntry, int, error)
	ResolveModelName(ctx context.Context, planCode string) (string, error)
	GetSKUChanges(ctx context.Context, limit int, changeType string) (map[string]any, []map[string]any, error)
}

// OrderQuoteResult holds exact cart price breakdown for a configuration.
type OrderQuoteResult struct {
	Price map[string]any `json:"price"`
}

// UnpaidOrderRequest specifies a strictly unpaid, single-quantity server order.
type UnpaidOrderRequest struct {
	TaskID                  string
	AccountID               string
	PlanCode                string
	Datacenter              string
	Options                 []string
	Quantity                int  // Must always be 1
	AutoPay                 bool // Must always be false
	WaiveRetractationPeriod bool // Must always be false
}

// UnpaidOrderResult returns the created unpaid order summary.
type UnpaidOrderResult struct {
	OrderID string         `json:"orderId"`
	Price   map[string]any `json:"price,omitempty"`
}

// ProcurementClient abstracts OVH cart quoting and unpaid order execution.
type ProcurementClient interface {
	QuoteOrder(ctx context.Context, accountID, planCode, datacenter string, options []string) (*OrderQuoteResult, error)
	CreateUnpaidOrder(ctx context.Context, req UnpaidOrderRequest) (*UnpaidOrderResult, error)
}

// MonitorClient abstracts stock subscription management on running background monitors.
type MonitorClient interface {
	AddStockSubscription(ctx context.Context, accountID, planCode string, datacenters []string, serverName string) error
	RemoveStockSubscription(ctx context.Context, accountID, planCode string) error
	AddVPSSubscription(ctx context.Context, planCode string) error
	RemoveVPSSubscription(ctx context.Context, planCodeOrID string) error
}

// MCPActionEvent carries structured audit notification data for L2/L3 mutations.
type MCPActionEvent struct {
	Tool      string         `json:"tool"`
	Scope     string         `json:"scope"`
	Summary   string         `json:"summary"`
	Arguments map[string]any `json:"arguments"`
	ClientIP  string         `json:"clientIp"`
	Timestamp string         `json:"timestamp"`
}

// NotificationEmitter dispatches async mcp.action notifications via internal/notify.
type NotificationEmitter interface {
	EmitMCPAction(ctx context.Context, evt MCPActionEvent)
}

// Dependencies holds the collaborators injected into NewService.
type Dependencies struct {
	Config      *config.Config
	Store       *store.Store
	OVHPool     *ovhclient.Pool
	Auth        *auth.Service
	TOTP        TOTPChecker // Optional override; defaults to Auth when nil
	Log         *logger.Hub
	Catalog     CatalogReader       // Optional; falls back to public OVH availability + SKU store when nil
	Procurement ProcurementClient   // Required for order_quote and order_create_unpaid
	Monitor     MonitorClient       // Optional; falls back to direct SQLite store operations when nil
	Notifier    NotificationEmitter // Optional; dispatches async L2/L3 mutation notifications
}
