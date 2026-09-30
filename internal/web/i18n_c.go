package web

import (
	"context"

	"github.com/MrCodeEU/glucava/internal/i18n"
)

// trCtx is the translator the withTranslator middleware put on ctx, English
// when there is none (a data builder called from a test or a background job).
func trCtx(ctx context.Context) *i18n.Translator {
	if tr, ok := ctx.Value(trKey{}).(*i18n.Translator); ok {
		return tr
	}
	return i18n.English()
}
