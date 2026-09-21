package migrations

import "github.com/pocketbase/pocketbase/core"

func init() {
	core.AppMigrations.Register(up003, nil, "003_session_lifetime.go")
}

// SessionSeconds is how long a login stays valid. The web cookie uses the same value.
const SessionSeconds = 3 * 24 * 3600

// up003 shortens PocketBase's default auth token lifetime for the users collection.
func up003(app core.App) error {
	users, err := app.FindCollectionByNameOrId("users")
	if err != nil {
		return err
	}
	users.AuthToken.Duration = SessionSeconds
	return app.Save(users)
}
