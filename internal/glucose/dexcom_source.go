package glucose

import (
	"os"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

func init() {
	Register(Descriptor{
		ID:       "dexcom",
		LabelKey: i18n.Key("source.dexcom.label"),
		IntroKey: i18n.Key("settings.dexcom.intro"),
		Fields: []Field{
			{Key: "region", Kind: KindSelect, LabelKey: i18n.Key("settings.dexcom.region.label"), Default: "ous", LegacyKey: "dexcom_region", Options: []Option{
				{Value: "ous", LabelKey: i18n.Key("settings.dexcom.region.ous")},
				{Value: "us", LabelKey: i18n.Key("settings.dexcom.region.us")},
				{Value: "jp", LabelKey: i18n.Key("settings.dexcom.region.jp")},
			}},
			{Key: "username", Kind: KindText, LabelKey: i18n.Key("settings.dexcom.username.label"), Required: true, LegacyKey: "dexcom_username"},
			{Key: "password", Kind: KindPassword, LabelKey: i18n.Key("settings.dexcom.password.label"), Required: true, SecretName: "dexcom_password"},
		},
		Build: func(v Values) (Source, error) {
			base := map[string]string{"us": RegionUS, "jp": RegionJP}[v.Get("region")]
			if base == "" {
				base = RegionOUS
			}
			if u := os.Getenv("GLUCAVA_DEXCOM_URL"); u != "" { // set for tests
				base = u
			}
			return NewDexcomShare(base, v.Get("username"), v.Get("password")), nil
		},
	})
}
