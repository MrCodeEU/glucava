package secrets

// Names of the values stored in the vault.
const (
	NameStravaCookies  = "strava_cookies"
	NameDexcomPassword = "dexcom_password"
	NameNtfyToken      = "ntfy_token"
	NameWebhookSecret  = "webhook_secret"
	NameSMTPPassword   = "smtp_password"
	// NameVAPIDPrivate is the Web Push (VAPID) signing key, generated on first
	// use. The public half is derived from it, so only this one is stored.
	NameVAPIDPrivate = "vapid_private"
)
