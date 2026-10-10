package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up027, nil, "027_token_scopes.go")
}

// up027 gives every API token a list of scopes, so a token that fires the push
// trigger on a phone cannot also download a backup. Tokens created before this
// keep what they could do: the trigger and /metrics.
func up027(app core.App) error {
	col, err := app.FindCollectionByNameOrId("api_tokens")
	if err != nil {
		return err
	}
	col.Fields.Add(&core.TextField{Name: "scopes", Max: 200})
	if err := app.Save(col); err != nil {
		return err
	}
	recs, err := app.FindAllRecords("api_tokens")
	if err != nil {
		return err
	}
	for _, r := range recs {
		r.Set("scopes", "trigger,metrics")
		if err := app.SaveNoValidate(r); err != nil {
			return err
		}
	}
	return nil
}
