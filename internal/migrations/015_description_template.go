package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up015, nil, "015_description_template.go")
}

// up015 adds the custom description template: a Go text/template (see
// render.RenderBlock). Empty means render.DefaultTemplate, so every
// existing deployment keeps today's exact wording until someone opts in.
func up015(app core.App) error {
	settings, err := app.FindCollectionByNameOrId("settings")
	if err != nil {
		return err
	}
	settings.Fields.Add(&core.TextField{Name: "description_template"})
	return app.Save(settings)
}
