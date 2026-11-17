package store

// StockTarget represents one hardware configuration and datacenter monitoring target within a StockSubscription.
type StockTarget struct {
	ID                string            `json:"id"`
	ConfigKey         string            `json:"configKey"`         // "" = any configuration (allowed ONLY when AutoOrder == false)
	Options           []string          `json:"options"`           // e.g. ["ram-32g...", "softraid-2x450nvme..."]
	Label             string            `json:"label"`             // human-readable Memory + Storage summary
	Datacenters       []string          `json:"datacenters"`       // empty = all DCs
	NotifyAvailable   bool              `json:"notifyAvailable"`
	NotifyUnavailable bool              `json:"notifyUnavailable"`
	AutoOrder         bool              `json:"autoOrder"`
	Quantity          int               `json:"quantity"`        // default 1
	MaxMonthlyPrice   float64           `json:"maxMonthlyPrice"` // 0 = no limit, >0 = ex-VAT monthly price cap
	PricingMode       string            `json:"pricingMode,omitempty"`
	Duration          string            `json:"duration,omitempty"`
	Coupon            string            `json:"coupon,omitempty"`
	Currency          string            `json:"currency,omitempty"`
	LastStatus        map[string]string `json:"lastStatus"`
}

// OrderHistoryItem aliases PurchaseHistory for order history consumers.
type OrderHistoryItem = PurchaseHistory
