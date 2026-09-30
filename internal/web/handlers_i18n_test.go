package web

import (
	"strings"
	"testing"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// German requests get German toasts, including plurals, placeholders and the
// store's validation problems.
func TestActionToastsFollowAcceptLanguage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	c := e.login(t)
	de := i18n.Default().For("de")
	hdr := map[string]string{"Accept-Language": "de-AT,de;q=0.9"}

	cases := []struct {
		name, path, body, want string
	}{
		{"plural and count", "/actions/strava/cookies", `{"cookies":"Cookie: _strava4_session=VALUE; sp=xyz"}`, de.Tn("toast.cookies.stored", 2)},
		{"plain error", "/actions/data/purge", `{"purgeConfirm":"nope"}`, de.T("err.purge.confirm")},
		{"validation problem", "/actions/settings", strings.Replace(validSettings, `"unit":"mmol/L"`, `"unit":"furlong"`, 1), de.T("validate.unit")},
		{"artifact verdict", "/actions/artifact-mark?start=1&end=2&kind=bogus", "{}", de.T("err.artifact.verdict")},
	}
	for _, tc := range cases {
		w := e.action(tc.path, tc.body, c, hdr)
		body := w.Body.String()
		if !strings.Contains(body, tc.want) {
			t.Errorf("%s: want %q in %s", tc.name, tc.want, body)
		}
		if strings.Contains(body, "Type DELETE to confirm") || strings.Contains(body, "Unit must be") {
			t.Errorf("%s: English leaked: %s", tc.name, body)
		}
	}
	if de.Tn("toast.cookies.stored", 2) != "2 Cookies gespeichert." {
		t.Errorf("cookie plural = %q", de.Tn("toast.cookies.stored", 2))
	}
}

// Changing a setting needs an English fallback on the CLI, where there is no
// request: Validate keeps returning English text.
func TestConfigValidateStaysEnglish(t *testing.T) {
	t.Parallel()
	v := settingsSignals{Unit: "furlong"}
	if got, want := v.config().Validate(), i18n.English().T("validate.unit"); got != want || got == "" {
		t.Errorf("Validate = %q, want %q", got, want)
	}
	if p, ok := v.check(); ok || p.Key != "validate.unit" {
		t.Errorf("check = %+v, %v", p, ok)
	}
}
