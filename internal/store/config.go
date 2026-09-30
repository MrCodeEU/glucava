package store

import (
	"fmt"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/MrCodeEU/glucava/internal/chartimg"
	"github.com/MrCodeEU/glucava/internal/render"
	"github.com/MrCodeEU/glucava/internal/stats"
)

// Range returns the target range with the very-low/very-high thresholds, the
// shape stats.Summarize and internal/analytics take.
func (c Config) Range() stats.Range {
	return stats.Range{Low: c.RangeLow, High: c.RangeHigh, VeryLow: c.VeryLow, VeryHigh: c.VeryHigh}
}

// validThresholds checks very low < target low and target high < very high.
// Zero means the default (54/250) and is not checked, so a target range that
// sits inside the defaults still saves.
func (c Config) validThresholds() bool {
	if c.VeryLow != 0 && (c.VeryLow < 20 || c.VeryLow >= c.RangeLow) {
		return false
	}
	if c.VeryHigh != 0 && (c.VeryHigh <= c.RangeHigh || c.VeryHigh > 600) {
		return false
	}
	return true
}

// Validate returns a message for the first problem with c, or "". It is the
// single rule set for every way of changing settings (web UI, CLI, env seeds).
func (c Config) Validate() string {
	switch {
	case c.Unit != "mg/dL" && c.Unit != "mmol/L":
		return "Unit must be mg/dL or mmol/L."
	case c.RangeLow < 40 || c.RangeLow > 200:
		return "Target low must be between 40 and 200 mg/dL."
	case c.RangeHigh <= c.RangeLow || c.RangeHigh > 400:
		return "Target high must be above the low value and at most 400 mg/dL."
	case !c.validThresholds():
		return "Very low must be between 20 and the target low, and very high between the target high and 600 mg/dL."
	case c.PreMin < 0 || c.PreMin > 240 || c.PostMin < 0 || c.PostMin > 240:
		return "Minutes before and after must be between 0 and 240."
	case c.ChartTheme != "light" && c.ChartTheme != "dark":
		return "Chart theme must be light or dark."
	case c.ChartSize != "standard" && c.ChartSize != "large":
		return "Chart size must be standard or large."
	case c.ChartPreMin < 0 || c.ChartPreMin > 240:
		return "Chart lead-in must be between 0 and 240 minutes."
	case c.ChartLine < 1 || c.ChartLine > 4:
		return "Chart line thickness must be between 1 and 4."
	case c.PollMin < 1 || c.PollMin > 1440:
		return "The polling interval must be between 1 and 1440 minutes."
	case c.PostBufferMin < 0 || c.PostBufferMin > 180:
		return "The delayed reprocess buffer must be between 0 and 180 minutes."
	case c.RetentionDays < 0 || c.RetentionDays > 3650:
		return "Retention must be between 0 and 3650 days."
	case c.DexcomRegion != "us" && c.DexcomRegion != "ous" && c.DexcomRegion != "jp":
		return "Choose a Dexcom region."
	case !validOptionalURL(c.NtfyURL):
		return "The ntfy URL must start with http:// or https://."
	case !validOptionalURL(c.WebhookURL):
		return "The webhook URL must start with http:// or https://."
	case !validOptionalEmail(c.EmailTo):
		return "The notification email address is not valid."
	case c.SMTPPort < 0 || c.SMTPPort > 65535:
		return "The SMTP port must be between 1 and 65535."
	case !validOptionalEmail(c.SMTPSender):
		return "The SMTP sender address is not valid."
	case c.GapAlertHours < 0 || c.GapAlertHours > 168:
		return "The glucose gap alert must be between 0 and 168 hours."
	case !validOptionalURL(c.PublicURL):
		return "The public URL must start with http:// or https://."
	case c.SMTPHost != "" && (c.SMTPPort == 0 || c.SMTPSender == ""):
		return "SMTP needs a port and a sender address."
	case c.DescriptionTemplate != "":
		if err := render.CheckTemplate(c.DescriptionTemplate); err != nil {
			return "Description template: " + err.Error()
		}
	case !ValidChartPanelOrder(c.ChartPanelOrder):
		return "Chart panel order must only list activity, band, dots, hr."
	case !validArtifactMode(c.ArtifactMode):
		return "Suspected artifacts must be handled as flagged or exclude."
	case !validOverviewRange(c.OverviewDefaultRange):
		return "The default Overview range must be one of " + strings.Join(OverviewRanges, ", ") + "."
	}
	if _, err := ParseOverviewLayout(c.OverviewLayout); err != nil {
		return "The Overview layout is not valid JSON: " + err.Error()
	}
	return ""
}

// ValidChartPanelOrder reports whether s is empty or a comma-separated list
// of only valid chart panel names (see chartPanelTokens). Exported so a
// caller building a Config from just one field (e.g. a chart preview's
// query-parameter override) can validate it without a whole Config's other
// fields also needing to be valid first.
func ValidChartPanelOrder(s string) bool {
	if s == "" {
		return true
	}
	valid := map[string]bool{}
	for _, p := range chartPanelTokens {
		valid[p] = true
	}
	for _, p := range strings.Split(s, ",") {
		if !valid[strings.TrimSpace(p)] {
			return false
		}
	}
	return true
}

func validOptionalURL(s string) bool {
	if s == "" {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func validOptionalEmail(s string) bool {
	if s == "" {
		return true
	}
	_, err := mail.ParseAddress(s)
	return err == nil
}

// configKey is one settings key as scripts see it: the same snake_case name
// as the settings column, with typed accessors.
type configKey struct {
	get func(*Config) string
	set func(*Config, string) error
}

func strKey(f func(*Config) *string) configKey {
	return configKey{
		get: func(c *Config) string { return *f(c) },
		set: func(c *Config, v string) error { *f(c) = v; return nil },
	}
}

func intKey(f func(*Config) *int) configKey {
	return configKey{
		get: func(c *Config) string { return strconv.Itoa(*f(c)) },
		set: func(c *Config, v string) error {
			n, err := strconv.Atoi(v)
			if err != nil {
				return fmt.Errorf("%q is not a whole number", v)
			}
			*f(c) = n
			return nil
		},
	}
}

func floatKey(f func(*Config) *float64) configKey {
	return configKey{
		get: func(c *Config) string { return strconv.FormatFloat(*f(c), 'f', -1, 64) },
		set: func(c *Config, v string) error {
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return fmt.Errorf("%q is not a number", v)
			}
			*f(c) = n
			return nil
		},
	}
}

func boolKey(f func(*Config) *bool) configKey {
	return configKey{
		get: func(c *Config) string { return strconv.FormatBool(*f(c)) },
		set: func(c *Config, v string) error {
			switch strings.ToLower(v) {
			case "1", "true", "yes", "on":
				*f(c) = true
			case "0", "false", "no", "off", "":
				*f(c) = false
			default:
				return fmt.Errorf("%q is not true or false", v)
			}
			return nil
		},
	}
}

// configKeys lists every scriptable setting. Secrets are deliberately absent:
// they go through the vault (see secrets.Names).
var configKeys = map[string]configKey{
	"unit":                   strKey(func(c *Config) *string { return &c.Unit }),
	"range_low":              floatKey(func(c *Config) *float64 { return &c.RangeLow }),
	"range_high":             floatKey(func(c *Config) *float64 { return &c.RangeHigh }),
	"very_low":               floatKey(func(c *Config) *float64 { return &c.VeryLow }),
	"very_high":              floatKey(func(c *Config) *float64 { return &c.VeryHigh }),
	"pre_minutes":            intKey(func(c *Config) *int { return &c.PreMin }),
	"post_minutes":           intKey(func(c *Config) *int { return &c.PostMin }),
	"poll_interval_minutes":  intKey(func(c *Config) *int { return &c.PollMin }),
	"retention_days":         intKey(func(c *Config) *int { return &c.RetentionDays }),
	"dexcom_region":          strKey(func(c *Config) *string { return &c.DexcomRegion }),
	"dexcom_username":        strKey(func(c *Config) *string { return &c.DexcomUsername }),
	"ntfy_url":               strKey(func(c *Config) *string { return &c.NtfyURL }),
	"webhook_url":            strKey(func(c *Config) *string { return &c.WebhookURL }),
	"email_to":               strKey(func(c *Config) *string { return &c.EmailTo }),
	"smtp_host":              strKey(func(c *Config) *string { return &c.SMTPHost }),
	"smtp_port":              intKey(func(c *Config) *int { return &c.SMTPPort }),
	"smtp_username":          strKey(func(c *Config) *string { return &c.SMTPUsername }),
	"smtp_tls":               boolKey(func(c *Config) *bool { return &c.SMTPTLS }),
	"smtp_sender_address":    strKey(func(c *Config) *string { return &c.SMTPSender }),
	"smtp_sender_name":       strKey(func(c *Config) *string { return &c.SMTPSenderName }),
	"public_url":             strKey(func(c *Config) *string { return &c.PublicURL }),
	"mail_alerts":            boolKey(func(c *Config) *bool { return &c.MailAlerts }),
	"mail_activity":          boolKey(func(c *Config) *bool { return &c.MailActivity }),
	"mail_weekly":            boolKey(func(c *Config) *bool { return &c.MailWeekly }),
	"mail_health":            boolKey(func(c *Config) *bool { return &c.MailHealth }),
	"push_alerts":            boolKey(func(c *Config) *bool { return &c.PushAlerts }),
	"push_summaries":         boolKey(func(c *Config) *bool { return &c.PushSummaries }),
	"gap_alert_hours":        intKey(func(c *Config) *int { return &c.GapAlertHours }),
	"chart_image":            boolKey(func(c *Config) *bool { return &c.ChartImage }),
	"chart_theme":            strKey(func(c *Config) *string { return &c.ChartTheme }),
	"chart_size":             strKey(func(c *Config) *string { return &c.ChartSize }),
	"chart_band":             boolKey(func(c *Config) *bool { return &c.ChartBand }),
	"chart_activity":         boolKey(func(c *Config) *bool { return &c.ChartActivity }),
	"chart_dots":             boolKey(func(c *Config) *bool { return &c.ChartDots }),
	"chart_line":             intKey(func(c *Config) *int { return &c.ChartLine }),
	"chart_hr":               boolKey(func(c *Config) *bool { return &c.ChartHR }),
	"chart_elevation":        boolKey(func(c *Config) *bool { return &c.ChartElevation }),
	"chart_panel_order":      strKey(func(c *Config) *string { return &c.ChartPanelOrder }),
	"chart_pre_minutes":      intKey(func(c *Config) *int { return &c.ChartPreMin }),
	"chart_avg_line":         boolKey(func(c *Config) *bool { return &c.ChartAvgLine }),
	"chart_range_lines":      boolKey(func(c *Config) *bool { return &c.ChartRangeLines }),
	"chart_min_max":          boolKey(func(c *Config) *bool { return &c.ChartMinMax }),
	"chart_hide_stats":       boolKey(func(c *Config) *bool { return &c.ChartHideStats }),
	"hr_read":                boolKey(func(c *Config) *bool { return &c.HRRead }),
	"post_buffer_minutes":    intKey(func(c *Config) *int { return &c.PostBufferMin }),
	"description_template":   strKey(func(c *Config) *string { return &c.DescriptionTemplate }),
	"overview_layout":        strKey(func(c *Config) *string { return &c.OverviewLayout }),
	"overview_default_range": strKey(func(c *Config) *string { return &c.OverviewDefaultRange }),
	"artifact_mode":          strKey(func(c *Config) *string { return &c.ArtifactMode }),
}

// ConfigKeys returns the scriptable setting names, sorted.
func ConfigKeys() []string {
	out := make([]string, 0, len(configKeys))
	for k := range configKeys {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Get returns the value of setting key as text.
func (c Config) Get(key string) (string, error) {
	k, ok := configKeys[key]
	if !ok {
		return "", fmt.Errorf("unknown setting %q (known: %s)", key, strings.Join(ConfigKeys(), ", "))
	}
	return k.get(&c), nil
}

// Set changes setting key from text. It only parses; call Validate afterwards,
// once all related keys (e.g. smtp_host and smtp_sender_address) are set.
func (c *Config) Set(key, value string) error {
	k, ok := configKeys[key]
	if !ok {
		return fmt.Errorf("unknown setting %q (known: %s)", key, strings.Join(ConfigKeys(), ", "))
	}
	if err := k.set(c, value); err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return nil
}

// ChartStyle converts the chart settings to how the chart is drawn.
// chartPanelTokens is every valid chartimg panel name, in the default draw
// order — the fallback order for a panel enabled but missing from (or
// misspelled in) ChartPanelOrder, so a stale order string can never hide a
// panel its own boolean turned on.
var chartPanelTokens = []string{chartimg.PanelActivity, chartimg.PanelBand, chartimg.PanelElevation, chartimg.PanelDots, chartimg.PanelHR}

// ChartStyle converts the chart settings to how the chart is drawn:
// ChartBand/ChartActivity/ChartDots/ChartHR/ChartElevation decide which
// panels are on, ChartPanelOrder (comma-separated panel names) decides the
// draw order among the ones that are.
func (c Config) ChartStyle() chartimg.Style {
	enabled := map[string]bool{
		chartimg.PanelActivity: c.ChartActivity, chartimg.PanelBand: c.ChartBand,
		chartimg.PanelDots: c.ChartDots, chartimg.PanelHR: c.ChartHR, chartimg.PanelElevation: c.ChartElevation,
	}
	seen := map[string]bool{}
	panels := []string{} // never nil: nil would mean "use chartimg's own default", not "none configured"
	for _, p := range append(strings.Split(c.ChartPanelOrder, ","), chartPanelTokens...) {
		p = strings.TrimSpace(p)
		if enabled[p] && !seen[p] {
			panels, seen[p] = append(panels, p), true
		}
	}
	return chartimg.Style{
		Dark: c.ChartTheme == "dark", Large: c.ChartSize == "large",
		Panels: panels, LineWidth: float64(c.ChartLine),
		AvgLine: c.ChartAvgLine, RangeLines: c.ChartRangeLines, MinMax: c.ChartMinMax, HideStats: c.ChartHideStats,
	}
}
