package web

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/MrCodeEU/glucava/internal/backup"
	"github.com/MrCodeEU/glucava/internal/tokens"
)

// exportBackup serves a backup zip without the encryption key. It takes either
// a signed-in session (the Settings button) or a bearer token with the backup
// scope (cron jobs), so a token that only fires the push trigger cannot read
// the data. Reading the key needs shell access: "glucava backup --include-key".
func (s *Server) exportBackup(w http.ResponseWriter, r *http.Request) {
	allowed := false
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
		ok, err := s.Tokens.VerifyScope(strings.TrimSpace(h[7:]), tokens.ScopeBackup)
		if err != nil {
			s.serverError(w, err)
			return
		}
		allowed = ok
	} else {
		_, allowed = s.user(r)
	}
	if !allowed {
		w.Header().Set("WWW-Authenticate", `Bearer realm="glucava backup"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Built in a temp file first, so a failure is a proper error and not a truncated zip.
	tmp, err := os.CreateTemp(s.App.DataDir(), "backup-*.zip")
	if err != nil {
		s.serverError(w, err)
		return
	}
	defer func() { _ = os.Remove(tmp.Name()); _ = tmp.Close() }()
	if err := backup.Create(r.Context(), s.App, tmp, s.Build, false); err != nil {
		s.serverError(w, err)
		return
	}
	now := time.Now()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="glucava-backup-`+now.Format("20060102-150405")+`.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", now, tmp)
}
