package store

import (
	"fmt"
	"net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

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
	case c.PreMin < 0 || c.PreMin > 240 || c.PostMin < 0 || c.PostMin > 240:
		return "Minutes before and after must be between 0 and 240."
	case c.PollMin < 1 || c.PollMin > 1440:
		return "The polling interval must be between 1 and 1440 minutes."
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
	case !validOptionalURL(c.PublicURL):
		return "The public URL must start with http:// or https://."
	case c.SMTPHost != "" && (c.SMTPPort == 0 || c.SMTPSender == ""):
		return "SMTP needs a port and a sender address."
	}
	return ""
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
	"unit":                  strKey(func(c *Config) *string { return &c.Unit }),
	"range_low":             floatKey(func(c *Config) *float64 { return &c.RangeLow }),
	"range_high":            floatKey(func(c *Config) *float64 { return &c.RangeHigh }),
	"pre_minutes":           intKey(func(c *Config) *int { return &c.PreMin }),
	"post_minutes":          intKey(func(c *Config) *int { return &c.PostMin }),
	"poll_interval_minutes": intKey(func(c *Config) *int { return &c.PollMin }),
	"retention_days":        intKey(func(c *Config) *int { return &c.RetentionDays }),
	"dexcom_region":         strKey(func(c *Config) *string { return &c.DexcomRegion }),
	"dexcom_username":       strKey(func(c *Config) *string { return &c.DexcomUsername }),
	"ntfy_url":              strKey(func(c *Config) *string { return &c.NtfyURL }),
	"webhook_url":           strKey(func(c *Config) *string { return &c.WebhookURL }),
	"email_to":              strKey(func(c *Config) *string { return &c.EmailTo }),
	"smtp_host":             strKey(func(c *Config) *string { return &c.SMTPHost }),
	"smtp_port":             intKey(func(c *Config) *int { return &c.SMTPPort }),
	"smtp_username":         strKey(func(c *Config) *string { return &c.SMTPUsername }),
	"smtp_tls":              boolKey(func(c *Config) *bool { return &c.SMTPTLS }),
	"smtp_sender_address":   strKey(func(c *Config) *string { return &c.SMTPSender }),
	"smtp_sender_name":      strKey(func(c *Config) *string { return &c.SMTPSenderName }),
	"public_url":            strKey(func(c *Config) *string { return &c.PublicURL }),
	"mail_alerts":           boolKey(func(c *Config) *bool { return &c.MailAlerts }),
	"mail_activity":         boolKey(func(c *Config) *bool { return &c.MailActivity }),
	"mail_weekly":           boolKey(func(c *Config) *bool { return &c.MailWeekly }),
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
