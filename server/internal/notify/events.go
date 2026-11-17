package notify

import (
	"fmt"
	"html"
	"strings"
	"time"

	"ovh-nexus/server/internal/catalog"
)

// Canonical outbound notification event keys.
const (
	EventStockAvailable   = "stock.available"
	EventStockUnavailable = "stock.unavailable"
	EventOrderSuccess     = "order.success"
	EventOrderFailed      = "order.failed"
	EventSKUChanged       = "sku.changed"
	EventServiceExpiring  = "service.expiring"
	EventOrderDelivered   = "order.delivered"
	EventMCPAction        = "mcp.action"
)

// Supported locales for outbound notification templates.
const (
	LocaleEN   = "en"
	LocaleFR   = "fr"
	LocaleZHCN = "zh-CN"
)

// EventDefinition describes a supported notification event for the UI and validation.
type EventDefinition struct {
	Key            string `json:"key"`
	RequiresAPIKey bool   `json:"requiresApiKey"`
	DefaultEnabled bool   `json:"defaultEnabled"`
	Label          string `json:"label"`
	Description    string `json:"description"`
}

// AllEvents returns the 8 supported notification event definitions.
func AllEvents() []EventDefinition {
	return []EventDefinition{
		{
			Key:            EventStockAvailable,
			RequiresAPIKey: false,
			DefaultEnabled: true,
			Label:          "Stock Restocked",
			Description:    "Triggered when a monitored dedicated server or VPS transitions into stock.",
		},
		{
			Key:            EventStockUnavailable,
			RequiresAPIKey: false,
			DefaultEnabled: false,
			Label:          "Stock Depleted",
			Description:    "Triggered when a monitored server or VPS transitions out of stock.",
		},
		{
			Key:            EventOrderSuccess,
			RequiresAPIKey: true,
			DefaultEnabled: true,
			Label:          "Order Created",
			Description:    "Triggered when the procurement queue or quick order creates an OVH order.",
		},
		{
			Key:            EventOrderFailed,
			RequiresAPIKey: true,
			DefaultEnabled: true,
			Label:          "Order Failed",
			Description:    "Triggered when an OVH cart checkout attempt returns an API error.",
		},
		{
			Key:            EventSKUChanged,
			RequiresAPIKey: false,
			DefaultEnabled: true,
			Label:          "Catalog SKU Changed",
			Description:    "Triggered when public OVH SKUs are added, removed, or reconfigured.",
		},
		{
			Key:            EventServiceExpiring,
			RequiresAPIKey: true,
			DefaultEnabled: false,
			Label:          "Service Expiring Soon",
			Description:    "Triggered when a dedicated server or VPS expires within the configured threshold.",
		},
		{
			Key:            EventOrderDelivered,
			RequiresAPIKey: true,
			DefaultEnabled: true,
			Label:          "Order Delivered",
			Description:    "Triggered when a placed order finishes provisioning and becomes delivered.",
		},
		{
			Key:            EventMCPAction,
			RequiresAPIKey: false,
			DefaultEnabled: true,
			Label:          "MCP AI Action",
			Description:    "Triggered when an AI client invokes L2 procurement or L3 operational MCP tools.",
		},
	}
}

// DefaultChannelEvents returns the event keys enabled by default on newly created channels.
func DefaultChannelEvents() []string {
	return []string{
		EventStockAvailable,
		EventOrderSuccess,
		EventOrderFailed,
		EventSKUChanged,
		EventOrderDelivered,
		EventMCPAction,
	}
}

// IsValidEvent reports whether eventType is one of the 8 supported event keys.
func IsValidEvent(eventType string) bool {
	for _, ev := range AllEvents() {
		if ev.Key == eventType {
			return true
		}
	}
	return false
}

// NormalizeLocale normalizes locale strings into "en", "fr", or "zh-CN", defaulting to "en".
func NormalizeLocale(raw string) string {
	cleaned := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(cleaned, "fr"):
		return LocaleFR
	case strings.HasPrefix(cleaned, "zh"):
		return LocaleZHCN
	default:
		return LocaleEN
	}
}

// Field represents a structured key-value row displayed in email tables and Telegram messages.
type Field struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

// EventOption configures optional metadata passed to EmitEvent.
type EventOption func(*eventConfig)

type eventConfig struct {
	templateData map[string]any
	badgeColor   string
	timestamp    time.Time
}

// WithTemplateData attaches structured parameters so notify can render localized templates.
func WithTemplateData(data map[string]any) EventOption {
	return func(c *eventConfig) {
		if len(data) == 0 {
			return
		}
		if c.templateData == nil {
			c.templateData = make(map[string]any, len(data))
		}
		for k, v := range data {
			c.templateData[k] = v
		}
	}
}

// WithBadgeColor overrides the header accent color in HTML emails.
func WithBadgeColor(color string) EventOption {
	return func(c *eventConfig) {
		if strings.TrimSpace(color) != "" {
			c.badgeColor = strings.TrimSpace(color)
		}
	}
}

// WithTimestamp overrides the notification timestamp (converted to time.Local when rendered).
func WithTimestamp(ts time.Time) EventOption {
	return func(c *eventConfig) {
		if !ts.IsZero() {
			c.timestamp = ts
		}
	}
}

// RenderedNotification holds the localized text and HTML payloads for a single dispatch.
type RenderedNotification struct {
	Locale       string
	EventType    string
	Subject      string
	Title        string
	PlainText    string
	HTMLBody     string
	TelegramText string
	Fields       []Field
	PanelURL     string
	Timestamp    string
}

type localeStrings struct {
	HTMLLang       string
	BrandHeader    string
	CTAText        string
	OpenConsoleLbl string
	FooterPrefix   string
	FooterSuffix   string
	TestTitle      string
	TestBody       string
	TestChannelLbl string
	TestTypeLbl    string
	EventNames     map[string]string
}

var translations = map[string]localeStrings{
	LocaleEN: {
		HTMLLang:       "en",
		BrandHeader:    "OVH Nexus · Cloud Fleet Notification",
		CTAText:        "Open OVH Nexus Console",
		OpenConsoleLbl: "Open OVH Nexus",
		FooterPrefix:   "Sent at",
		FooterSuffix:   "Automated alert by OVH Nexus",
		TestTitle:      "Notification Channel Test",
		TestBody:       "Your OVH Nexus notification channel is configured and delivering messages correctly.",
		TestChannelLbl: "Channel",
		TestTypeLbl:    "Provider",
		EventNames: map[string]string{
			EventStockAvailable:   "Stock Available",
			EventStockUnavailable: "Stock Unavailable",
			EventOrderSuccess:     "Order Created",
			EventOrderFailed:      "Order Failed",
			EventSKUChanged:       "Catalog SKU Changed",
			EventServiceExpiring:  "Service Expiring",
			EventOrderDelivered:   "Order Delivered",
			EventMCPAction:        "MCP Action Audit",
		},
	},
	LocaleFR: {
		HTMLLang:       "fr",
		BrandHeader:    "OVH Nexus · Notification Cloud Fleet",
		CTAText:        "Ouvrir la console OVH Nexus",
		OpenConsoleLbl: "Ouvrir OVH Nexus",
		FooterPrefix:   "Envoyé le",
		FooterSuffix:   "Notification automatique par OVH Nexus",
		TestTitle:      "Test du canal de notification",
		TestBody:       "Votre canal de notification OVH Nexus est configuré et fonctionne correctement.",
		TestChannelLbl: "Canal",
		TestTypeLbl:    "Fournisseur",
		EventNames: map[string]string{
			EventStockAvailable:   "Stock disponible",
			EventStockUnavailable: "Rupture de stock",
			EventOrderSuccess:     "Commande créée",
			EventOrderFailed:      "Échec de commande",
			EventSKUChanged:       "Changement de SKU",
			EventServiceExpiring:  "Service bientôt expiré",
			EventOrderDelivered:   "Commande livrée",
			EventMCPAction:        "Audit d'action MCP",
		},
	},
	LocaleZHCN: {
		HTMLLang:       "zh-CN",
		BrandHeader:    "OVH Nexus · Cloud Fleet 通知中心",
		CTAText:        "打开 OVH Nexus 控制台",
		OpenConsoleLbl: "打开 OVH Nexus",
		FooterPrefix:   "发送时间",
		FooterSuffix:   "由 OVH Nexus 自动推送",
		TestTitle:      "通知渠道测试",
		TestBody:       "您的 OVH Nexus 通知渠道配置正常，消息投递测试成功。",
		TestChannelLbl: "渠道名称",
		TestTypeLbl:    "渠道类型",
		EventNames: map[string]string{
			EventStockAvailable:   "库存补货",
			EventStockUnavailable: "库存售罄",
			EventOrderSuccess:     "下单成功",
			EventOrderFailed:      "下单失败",
			EventSKUChanged:       "SKU 货架变动",
			EventServiceExpiring:  "服务即将到期",
			EventOrderDelivered:   "订单交付完成",
			EventMCPAction:        "MCP AI 操作审计",
		},
	},
}

// RenderNotification renders localized email HTML, plain text, and Telegram Markdown payloads.
func RenderNotification(
	locale string,
	eventType string,
	title string,
	plainText string,
	fields []Field,
	panelURL string,
	data map[string]any,
	ts time.Time,
) RenderedNotification {
	loc := NormalizeLocale(locale)
	tr := translations[loc]

	if ts.IsZero() {
		ts = time.Now()
	}
	localTS := ts.In(time.Local).Format("2006-01-02 15:04:05 MST")

	cleanPanel := sanitizeHTTPURL(panelURL)
	resolvedTitle, resolvedBody, resolvedFields := resolveTemplateContent(loc, eventType, title, plainText, fields, data, localTS)

	subject := fmt.Sprintf("[OVH Nexus] %s", resolvedTitle)
	emailPlain := resolvedBody
	if len(resolvedFields) > 0 {
		var sb strings.Builder
		sb.WriteString(resolvedTitle)
		sb.WriteString("\n\n")
		for _, f := range resolvedFields {
			sb.WriteString(f.Label)
			sb.WriteString(": ")
			sb.WriteString(f.Value)
			sb.WriteByte('\n')
		}
		if resolvedBody != "" {
			sb.WriteByte('\n')
			sb.WriteString(resolvedBody)
		}
		emailPlain = sb.String()
	}
	if cleanPanel != "" {
		emailPlain = fmt.Sprintf("%s\n\n---\n%s: %s", emailPlain, tr.OpenConsoleLbl, cleanPanel)
	}
	emailPlain = fmt.Sprintf("%s\n%s: %s", emailPlain, tr.FooterPrefix, localTS)

	htmlBody := renderHTMLBody(tr, eventType, resolvedTitle, resolvedBody, resolvedFields, cleanPanel, localTS)
	tgText := renderTelegramText(tr, eventType, resolvedTitle, resolvedBody, resolvedFields, cleanPanel, localTS)

	return RenderedNotification{
		Locale:       loc,
		EventType:    eventType,
		Subject:      subject,
		Title:        resolvedTitle,
		PlainText:    emailPlain,
		HTMLBody:     htmlBody,
		TelegramText: tgText,
		Fields:       resolvedFields,
		PanelURL:     cleanPanel,
		Timestamp:    localTS,
	}
}

func resolveTemplateContent(
	loc string,
	eventType string,
	title string,
	plainText string,
	fields []Field,
	data map[string]any,
	localTS string,
) (string, string, []Field) {
	if len(data) > 0 {
		tTitle, tBody, tFields := buildLocalizedTemplate(loc, eventType, data, localTS)
		if strings.TrimSpace(title) == "" || boolVal(data["preferTemplate"]) {
			title = tTitle
		}
		if strings.TrimSpace(plainText) == "" || boolVal(data["preferTemplate"]) {
			plainText = tBody
		}
		if len(fields) == 0 || boolVal(data["preferTemplate"]) {
			fields = tFields
		}
	}

	if strings.TrimSpace(title) == "" {
		tTitle, tBody, tFields := buildLocalizedTemplate(loc, eventType, data, localTS)
		title = tTitle
		if strings.TrimSpace(plainText) == "" {
			plainText = tBody
		}
		if len(fields) == 0 {
			fields = tFields
		}
	}
	if strings.TrimSpace(plainText) == "" {
		plainText = title
	}
	return title, plainText, fields
}

func buildLocalizedTemplate(loc, eventType string, data map[string]any, localTS string) (string, string, []Field) {
	str := func(key, fallback string) string {
		if data == nil {
			return fallback
		}
		if v, ok := data[key]; ok {
			s := strings.TrimSpace(fmt.Sprint(v))
			if s != "" && s != "<nil>" {
				return s
			}
		}
		return fallback
	}

	planCode := str("planCode", "N/A")
	modelName := str("modelName", planCode)
	rawDC := str("datacenter", str("datacenters", "ALL"))
	dc := strings.ToUpper(rawDC)
	if !strings.EqualFold(rawDC, "ALL") && !strings.EqualFold(rawDC, "N/A") {
		dc = catalog.FormatDatacenterLabel(rawDC, loc)
	}
	orderID := str("orderId", "N/A")
	orderURL := str("orderUrl", "")
	priceText := str("price", "")
	errMsg := str("error", "")
	serviceName := str("serviceName", "N/A")
	serviceType := str("serviceType", "dedicated")
	expDate := str("expiration", "N/A")
	daysLeft := str("daysRemaining", "0")
	renewMode := str("renewMode", "manual")
	toolName := str("tool", "unknown")
	scope := str("scope", "read")
	summary := str("summary", "")
	clientIP := str("clientIp", "unknown")
	duration := str("duration", "")
	specs := str("specs", "")
	endpoint := strings.ToUpper(str("endpoint", "OVH-EU"))
	addedCount := str("addedCount", "0")
	removedCount := str("removedCount", "0")
	changedCount := str("changedCount", "0")

	switch eventType {
	case EventStockAvailable:
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Modèle", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter(s)", Value: dc},
			}
			if specs != "" {
				fields = append(fields, Field{Label: "Configuration", Value: specs})
			}
			if priceText != "" {
				fields = append(fields, Field{Label: "Prix", Value: priceText})
			}
			body := fmt.Sprintf("Le serveur %s (%s) est désormais disponible dans : %s.", modelName, planCode, dc)
			return fmt.Sprintf("Stock disponible : %s (%s)", modelName, dc), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "服务器型号", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "可用机房", Value: dc},
			}
			if specs != "" {
				fields = append(fields, Field{Label: "硬件配置", Value: specs})
			}
			if priceText != "" {
				fields = append(fields, Field{Label: "参考价格", Value: priceText})
			}
			body := fmt.Sprintf("服务器 %s (%s) 已在机房 %s 恢复库存。", modelName, planCode, dc)
			return fmt.Sprintf("服务器补货通知：%s (%s)", modelName, dc), body, fields
		default:
			fields := []Field{
				{Label: "Model", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter(s)", Value: dc},
			}
			if specs != "" {
				fields = append(fields, Field{Label: "Configuration", Value: specs})
			}
			if priceText != "" {
				fields = append(fields, Field{Label: "Price", Value: priceText})
			}
			body := fmt.Sprintf("Server %s (%s) is now available in %s.", modelName, planCode, dc)
			return fmt.Sprintf("Stock Available: %s (%s)", modelName, dc), body, fields
		}

	case EventStockUnavailable:
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Modèle", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
			}
			if duration != "" {
				fields = append(fields, Field{Label: "Durée en stock", Value: duration})
			}
			body := fmt.Sprintf("Le serveur %s (%s) n'est plus disponible dans %s.", modelName, planCode, dc)
			return fmt.Sprintf("Rupture de stock : %s (%s)", modelName, dc), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "服务器型号", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "机房", Value: dc},
			}
			if duration != "" {
				fields = append(fields, Field{Label: "有货持续时间", Value: duration})
			}
			body := fmt.Sprintf("服务器 %s (%s) 在机房 %s 已售罄。", modelName, planCode, dc)
			return fmt.Sprintf("服务器售罄通知：%s (%s)", modelName, dc), body, fields
		default:
			fields := []Field{
				{Label: "Model", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
			}
			if duration != "" {
				fields = append(fields, Field{Label: "In-Stock Duration", Value: duration})
			}
			body := fmt.Sprintf("Server %s (%s) is now out of stock in %s.", modelName, planCode, dc)
			return fmt.Sprintf("Stock Unavailable: %s (%s)", modelName, dc), body, fields
		}

	case EventOrderSuccess:
		autoPaidText := map[string]string{
			LocaleEN:   "Unpaid (Manual Checkout Required)",
			LocaleFR:   "Impayée (Paiement manuel requis)",
			LocaleZHCN: "待支付（未自动扣款）",
		}[loc]
		if boolVal(data["autoPaid"]) {
			autoPaidText = map[string]string{
				LocaleEN:   "Auto-Pay Requested",
				LocaleFR:   "Paiement automatique demandé",
				LocaleZHCN: "已触发自动扣款",
			}[loc]
		}
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Numéro de commande", Value: "#" + orderID},
				{Label: "Modèle", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
				{Label: "Paiement", Value: autoPaidText},
			}
			if priceText != "" {
				fields = append(fields, Field{Label: "Montant", Value: priceText})
			}
			if orderURL != "" {
				fields = append(fields, Field{Label: "Lien commande", Value: orderURL})
			}
			body := fmt.Sprintf("Commande #%s créée pour %s (%s) dans %s.", orderID, modelName, planCode, dc)
			return fmt.Sprintf("Commande créée #%s : %s", orderID, modelName), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "订单号", Value: "#" + orderID},
				{Label: "服务器型号", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "机房", Value: dc},
				{Label: "支付状态", Value: autoPaidText},
			}
			if priceText != "" {
				fields = append(fields, Field{Label: "订单价格", Value: priceText})
			}
			if orderURL != "" {
				fields = append(fields, Field{Label: "订单链接", Value: orderURL})
			}
			body := fmt.Sprintf("已成功创建订单 #%s（%s / %s @ %s）。", orderID, modelName, planCode, dc)
			return fmt.Sprintf("下单成功 #%s：%s", orderID, modelName), body, fields
		default:
			fields := []Field{
				{Label: "Order ID", Value: "#" + orderID},
				{Label: "Model", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
				{Label: "Payment Mode", Value: autoPaidText},
			}
			if priceText != "" {
				fields = append(fields, Field{Label: "Price", Value: priceText})
			}
			if orderURL != "" {
				fields = append(fields, Field{Label: "Order URL", Value: orderURL})
			}
			body := fmt.Sprintf("Order #%s created for %s (%s) in %s.", orderID, modelName, planCode, dc)
			return fmt.Sprintf("Order Created #%s: %s", orderID, modelName), body, fields
		}

	case EventOrderFailed:
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Modèle", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
				{Label: "Erreur", Value: errMsg},
			}
			body := fmt.Sprintf("Échec de création de commande pour %s (%s) : %s", modelName, planCode, errMsg)
			return fmt.Sprintf("Échec de commande : %s", modelName), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "服务器型号", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "机房", Value: dc},
				{Label: "失败原因", Value: errMsg},
			}
			body := fmt.Sprintf("服务器 %s (%s) 下单失败：%s", modelName, planCode, errMsg)
			return fmt.Sprintf("下单失败：%s", modelName), body, fields
		default:
			fields := []Field{
				{Label: "Model", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
				{Label: "Reason", Value: errMsg},
			}
			body := fmt.Sprintf("Checkout failed for %s (%s): %s", modelName, planCode, errMsg)
			return fmt.Sprintf("Order Failed: %s", modelName), body, fields
		}

	case EventSKUChanged:
		detailText := str("details", "")
		saleStartedCount := str("saleStartedCount", "0")
		replacedCount := str("replacedCount", "0")
		hasExtendedCounts := saleStartedCount != "0" || replacedCount != "0"
		onlySaleStarted := saleStartedCount != "0" && addedCount == "0" && removedCount == "0" && replacedCount == "0"
		switch loc {
		case LocaleFR:
			summaryVal := fmt.Sprintf("+%s ajoutés / -%s retirés / %s modifiés", addedCount, removedCount, changedCount)
			if hasExtendedCounts {
				summaryVal = fmt.Sprintf("+%s ajoutés / %s en vente / %s remplacés / -%s retirés / %s modifiés", addedCount, saleStartedCount, replacedCount, removedCount, changedCount)
			}
			fields := []Field{
				{Label: "Région API", Value: endpoint},
				{Label: "Résumé", Value: summaryVal},
			}
			body := detailText
			if body == "" {
				body = fmt.Sprintf("Changements détectés sur le catalogue public %s à %s.", endpoint, localTS)
			}
			if onlySaleStarted {
				return fmt.Sprintf("Ouverture des ventes SKU : %s modèle(s)", saleStartedCount), body, fields
			}
			return fmt.Sprintf("Changement SKU : +%s / -%s", addedCount, removedCount), body, fields
		case LocaleZHCN:
			summaryVal := fmt.Sprintf("上新 %s 款 / 下架 %s 款 / 配置更新 %s 款", addedCount, removedCount, changedCount)
			if hasExtendedCounts {
				summaryVal = fmt.Sprintf("新增 %s 款 / 开售 %s 款 / 换代 %s 款 / 下架 %s 款 / 配置更新 %s 款", addedCount, saleStartedCount, replacedCount, removedCount, changedCount)
			}
			fields := []Field{
				{Label: "API 区域", Value: endpoint},
				{Label: "变动统计", Value: summaryVal},
			}
			body := detailText
			if body == "" {
				body = fmt.Sprintf("检测到 %s 公开货架 SKU 发生变化（%s）。", endpoint, localTS)
			}
			if onlySaleStarted {
				return fmt.Sprintf("SKU 开售提醒：%s 款机型正式开售", saleStartedCount), body, fields
			}
			return fmt.Sprintf("SKU 变动提醒：上新 %s 款 / 下架 %s 款", addedCount, removedCount), body, fields
		default:
			summaryVal := fmt.Sprintf("+%s added / -%s removed / %s updated", addedCount, removedCount, changedCount)
			if hasExtendedCounts {
				summaryVal = fmt.Sprintf("+%s added / %s sale started / %s replaced / -%s removed / %s updated", addedCount, saleStartedCount, replacedCount, removedCount, changedCount)
			}
			fields := []Field{
				{Label: "API Endpoint", Value: endpoint},
				{Label: "Summary", Value: summaryVal},
			}
			body := detailText
			if body == "" {
				body = fmt.Sprintf("Public catalog SKU changes detected on %s at %s.", endpoint, localTS)
			}
			if onlySaleStarted {
				return fmt.Sprintf("SKU Sale Started: %s model(s)", saleStartedCount), body, fields
			}
			return fmt.Sprintf("SKU Catalog Update: +%s / -%s", addedCount, removedCount), body, fields
		}

	case EventServiceExpiring:
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Service", Value: serviceName},
				{Label: "Type", Value: serviceType},
				{Label: "Date d'expiration", Value: fmt.Sprintf("%s (%s jours restants)", expDate, daysLeft)},
				{Label: "Renouvellement", Value: renewMode},
			}
			body := fmt.Sprintf("Le service %s expire le %s (dans %s jours).", serviceName, expDate, daysLeft)
			return fmt.Sprintf("Expiration proche : %s (%sj)", serviceName, daysLeft), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "服务名称", Value: serviceName},
				{Label: "服务类型", Value: serviceType},
				{Label: "到期日期", Value: fmt.Sprintf("%s（剩余 %s 天）", expDate, daysLeft)},
				{Label: "续费模式", Value: renewMode},
			}
			body := fmt.Sprintf("服务 %s 将于 %s 到期（剩余 %s 天）。", serviceName, expDate, daysLeft)
			return fmt.Sprintf("服务即将到期：%s（剩余 %s 天）", serviceName, daysLeft), body, fields
		default:
			fields := []Field{
				{Label: "Service", Value: serviceName},
				{Label: "Type", Value: serviceType},
				{Label: "Expiration", Value: fmt.Sprintf("%s (%s days remaining)", expDate, daysLeft)},
				{Label: "Renewal Mode", Value: renewMode},
			}
			body := fmt.Sprintf("Service %s expires on %s (%s days remaining).", serviceName, expDate, daysLeft)
			return fmt.Sprintf("Service Expiring: %s (%sd left)", serviceName, daysLeft), body, fields
		}

	case EventOrderDelivered:
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Commande", Value: "#" + orderID},
				{Label: "Modèle", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
				{Label: "Statut", Value: "Livré (delivered)"},
			}
			body := fmt.Sprintf("La commande #%s (%s) a été livrée et est prête.", orderID, modelName)
			return fmt.Sprintf("Commande #%s livrée : %s", orderID, modelName), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "订单号", Value: "#" + orderID},
				{Label: "服务器型号", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "机房", Value: dc},
				{Label: "交付状态", Value: "已交付 (delivered)"},
			}
			body := fmt.Sprintf("订单 #%s（%s）已完成装机并交付使用。", orderID, modelName)
			return fmt.Sprintf("订单已交付 #%s：%s", orderID, modelName), body, fields
		default:
			fields := []Field{
				{Label: "Order ID", Value: "#" + orderID},
				{Label: "Model", Value: fmt.Sprintf("%s (%s)", modelName, planCode)},
				{Label: "Datacenter", Value: dc},
				{Label: "Status", Value: "Delivered"},
			}
			body := fmt.Sprintf("Order #%s (%s) has finished provisioning and is now delivered.", orderID, modelName)
			return fmt.Sprintf("Order #%s Delivered: %s", orderID, modelName), body, fields
		}

	case EventMCPAction:
		switch loc {
		case LocaleFR:
			fields := []Field{
				{Label: "Outil MCP", Value: toolName},
				{Label: "Portée", Value: scope},
				{Label: "IP client", Value: clientIP},
			}
			body := summary
			if body == "" {
				body = fmt.Sprintf("Le client AI a exécuté l'outil MCP %s (%s).", toolName, scope)
			}
			return fmt.Sprintf("Audit MCP : %s", toolName), body, fields
		case LocaleZHCN:
			fields := []Field{
				{Label: "MCP 工具", Value: toolName},
				{Label: "权限层级", Value: scope},
				{Label: "来源 IP", Value: clientIP},
			}
			body := summary
			if body == "" {
				body = fmt.Sprintf("AI 客户端通过 MCP 网关执行了 %s（%s）。", toolName, scope)
			}
			return fmt.Sprintf("MCP AI 操作审计：%s", toolName), body, fields
		default:
			fields := []Field{
				{Label: "MCP Tool", Value: toolName},
				{Label: "Permission Scope", Value: scope},
				{Label: "Client IP", Value: clientIP},
			}
			body := summary
			if body == "" {
				body = fmt.Sprintf("An AI client invoked MCP tool %s (%s).", toolName, scope)
			}
			return fmt.Sprintf("MCP Action Audit: %s", toolName), body, fields
		}

	default:
		return translations[loc].TestTitle, translations[loc].TestBody, nil
	}
}

func renderHTMLBody(
	tr localeStrings,
	eventType string,
	title string,
	body string,
	fields []Field,
	panelURL string,
	localTS string,
) string {
	safeTitle := html.EscapeString(title)
	safeBody := strings.ReplaceAll(html.EscapeString(body), "\n", "<br/>")
	safeTime := html.EscapeString(localTS)
	eventLabel := tr.EventNames[eventType]
	if eventLabel == "" {
		eventLabel = eventType
	}

	var rowsBuilder strings.Builder
	if len(fields) > 0 {
		rowsBuilder.WriteString(`<table width="100%" cellpadding="0" cellspacing="0" style="border:1px solid #E2E8F0;border-radius:8px;border-collapse:collapse;margin:16px 0;background:#F8FAFC;">`)
		for _, f := range fields {
			rowsBuilder.WriteString(`<tr>`)
			rowsBuilder.WriteString(`<td style="padding:8px 12px;border-bottom:1px solid #E2E8F0;color:#64748B;font-size:13px;width:32%;font-weight:600;">`)
			rowsBuilder.WriteString(html.EscapeString(f.Label))
			rowsBuilder.WriteString(`</td><td style="padding:8px 12px;border-bottom:1px solid #E2E8F0;color:#0F172A;font-size:13px;font-family:ui-monospace,SFMono-Regular,Menlo,monospace;">`)
			rowsBuilder.WriteString(html.EscapeString(f.Value))
			rowsBuilder.WriteString(`</td></tr>`)
		}
		rowsBuilder.WriteString(`</table>`)
	}

	var ctaHTML string
	if panelURL != "" {
		escapedURL := html.EscapeString(panelURL)
		ctaHTML = fmt.Sprintf(
			`<div style="margin-top:22px;text-align:left;"><a href="%s" target="_blank" rel="noopener noreferrer" style="display:inline-block;padding:10px 20px;background:#0050D7;color:#FFFFFF;text-decoration:none;border-radius:6px;font-size:13px;font-weight:600;">%s &rarr;</a></div>`,
			escapedURL,
			html.EscapeString(tr.CTAText),
		)
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="%s">
<head><meta charset="utf-8"/></head>
<body style="margin:0;padding:24px;background:#F1F5F9;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,sans-serif;color:#0F172A;">
  <div style="max-width:600px;margin:0 auto;background:#FFFFFF;border-radius:12px;border:1px solid #E2E8F0;overflow:hidden;box-shadow:0 4px 12px rgba(15,23,42,0.06);">
    <div style="background:#0050D7;padding:16px 24px;color:#FFFFFF;display:flex;align-items:center;justify-content:space-between;">
      <div style="font-size:16px;font-weight:700;letter-spacing:0.3px;">%s</div>
      <div style="font-size:12px;opacity:0.9;">%s</div>
    </div>
    <div style="padding:24px;">
      <h2 style="margin:0 0 14px 0;font-size:18px;color:#0F172A;">%s</h2>
      %s
      <div style="font-size:14px;line-height:1.65;color:#1E293B;background:#F8FAFC;padding:14px 16px;border-radius:8px;border:1px solid #E2E8F0;">%s</div>
      %s
    </div>
    <div style="padding:12px 24px;background:#F8FAFC;border-top:1px solid #E2E8F0;font-size:12px;color:#64748B;">
      %s: %s &middot; %s
    </div>
  </div>
</body>
</html>`,
		html.EscapeString(tr.HTMLLang),
		html.EscapeString(tr.BrandHeader),
		html.EscapeString(eventLabel),
		safeTitle,
		rowsBuilder.String(),
		safeBody,
		ctaHTML,
		html.EscapeString(tr.FooterPrefix),
		safeTime,
		html.EscapeString(tr.FooterSuffix),
	)
}

func renderTelegramText(
	tr localeStrings,
	eventType string,
	title string,
	body string,
	fields []Field,
	panelURL string,
	localTS string,
) string {
	eventLabel := tr.EventNames[eventType]
	if eventLabel == "" {
		eventLabel = eventType
	}

	var sb strings.Builder
	sb.WriteString("*")
	sb.WriteString(escapeTGMarkdown(title))
	sb.WriteString("*\n")
	sb.WriteString("_")
	sb.WriteString(escapeTGMarkdown(eventLabel))
	sb.WriteString("_\n")

	if len(fields) > 0 {
		sb.WriteByte('\n')
		for _, f := range fields {
			sb.WriteString("- *")
			sb.WriteString(escapeTGMarkdown(f.Label))
			sb.WriteString("*: ")
			sb.WriteString(escapeTGMarkdown(f.Value))
			sb.WriteByte('\n')
		}
	}

	if strings.TrimSpace(body) != "" {
		sb.WriteByte('\n')
		sb.WriteString(escapeTGMarkdown(body))
		sb.WriteByte('\n')
	}

	if panelURL != "" {
		sb.WriteByte('\n')
		sb.WriteString(escapeTGMarkdown(tr.OpenConsoleLbl))
		sb.WriteString(": ")
		sb.WriteString(escapeTGMarkdown(panelURL))
		sb.WriteByte('\n')
	}

	sb.WriteString("\n")
	sb.WriteString(escapeTGMarkdown(tr.FooterPrefix))
	sb.WriteString(": ")
	sb.WriteString(escapeTGMarkdown(localTS))
	return sb.String()
}

func escapeTGMarkdown(s string) string {
	// Legacy Telegram Markdown v1 escapes _, *, `, [
	r := strings.NewReplacer(
		`_`, `\_`,
		`*`, `\*`,
		"`", "\\`",
		`[`, `\[`,
	)
	return r.Replace(s)
}

func sanitizeHTTPURL(raw string) string {
	u := strings.TrimSpace(raw)
	if strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://") {
		return u
	}
	return ""
}

func boolVal(v any) bool {
	b, ok := v.(bool)
	return ok && b
}
