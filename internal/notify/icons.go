package notify

// icons are the emoji shown in the header of an email, by message type. Emoji
// need no remote images and render in every mail client.
var icons = map[string]string{
	"strava_failed":       "\U0001F6AB", // no entry
	"selector_repaired":   "\U0001F527", // wrench
	"session_expired":     "\U0001F511", // key
	"glucose_unavailable": "\U0001F4E1", // satellite antenna
	"canary_failed":       "\U0001F424", // canary-yellow chick
	"trigger_rejected":    "\U0001F6D1", // stop sign
	TypeTest:              "\U0001F9EA", // test tube
	TypeActivitySummary:   "\U0001F3C5", // medal
	TypeWeeklySummary:     "\U0001F4CA", // bar chart
}

// IconFor returns the header icon for a message: its own Icon, else the one
// for its type, else a bell.
func IconFor(m Message) string {
	if m.Icon != "" {
		return m.Icon
	}
	if i, ok := icons[m.Type]; ok {
		return i
	}
	return "\U0001F514"
}
