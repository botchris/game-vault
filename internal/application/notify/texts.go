package notify

import (
	"fmt"
	"strings"
)

// texts are the messages' texts, by language and key. Every language has the same keys (a test
// checks it); English is the fallback. The reason.* texts mirror the web's health.reasons.* in
// web/src/i18n/locales, with the same {{param}} placeholders.
var texts = map[string]map[string]string{
	"en": {
		"added.title.source":      "%s: %d new games",
		"added.title.source.one":  "%s: a new game",
		"added.title.manual":      "%d games added by hand",
		"added.title.manual.one":  "A game added by hand",
		"added.title.import":      "CSV import: %d new games",
		"added.copies":            "and %d new copies of games you had",
		"more":                    "… and %d more",
		"release.announced.title": "New in your series: %s",
		"release.out.title":       "Out today: %s",
		"release.because":         "Because you have %s",
		"release.tba":             "date to be announced",
		"deal.title":              "%s at its historical low",
		"deal.text":               "%s (−%d%%, was %s) at %s",
		"issue.opened.title":      "⚠ %s",
		"issue.resolved.title":    "✓ %s works again",
		"keys.title":              "%d keys expire within 30 days",
		"keys.title.one":          "A key expires within 30 days",
		"keys.line":               "%s (%s): %s",
		"days.today":              "today",
		"days.one":                "in 1 day",
		"days.other":              "in %d days",
		"logins.title":            "%d failed sign-ins from %s",
		"logins.text":             "Between %s and %s. If it was not you, check who can reach Game Vault.",
		"open":                    "Open in Game Vault",
		"open.page":               "Open the page",
		"test.title":              "Game Vault test",
		"test.text":               "This connection works. Example Game and Another Game would be listed like this.",
		"kind.backups":            "Backups",
		"kind.disk":               "Disk space",

		"reason.provider_quota":          "Monthly allowance used up; renews {{until}}",
		"reason.provider_rate_limited":   "Too many requests; trying again in a few minutes",
		"reason.provider_unavailable":    "Not answering; trying again in a few minutes",
		"reason.provider_settings":       "Rejects its settings: check them on the Providers page",
		"reason.source_signed_out":       "Signed out: sign in again in Sources",
		"reason.source_sync_failed":      "The last scan failed",
		"reason.source_keepalive_failed": "Keeping the session alive failed",
		"reason.source_list_failed":      "The sources could not be read to check them",
		"reason.backup_failed":           "The last backup failed: make one now in System → Backups",
		"reason.backup_stale":            "No backup in {{days}} days",
		"reason.disk_low":                "Only {{free}} free ({{percent}}%) on the disk with the data",
		"reason.disk_critical":           "Almost no space left: {{free}} free on the disk with the data",
		"reason.connector_paused":        "Notifications are paused: {{reason}}",
		"date.on":                        "on %s %d",
		"month.1":                        "January",
		"month.2":                        "February",
		"month.3":                        "March",
		"month.4":                        "April",
		"month.5":                        "May",
		"month.6":                        "June",
		"month.7":                        "July",
		"month.8":                        "August",
		"month.9":                        "September",
		"month.10":                       "October",
		"month.11":                       "November",
		"month.12":                       "December",
	},
	"es": {
		"added.title.source":      "%s: %d juegos nuevos",
		"added.title.source.one":  "%s: un juego nuevo",
		"added.title.manual":      "%d juegos añadidos a mano",
		"added.title.manual.one":  "Un juego añadido a mano",
		"added.title.import":      "Importación CSV: %d juegos nuevos",
		"added.copies":            "y %d copias nuevas de juegos que ya tenías",
		"more":                    "… y %d más",
		"release.announced.title": "Nuevo en tu saga: %s",
		"release.out.title":       "Hoy sale %s",
		"release.because":         "Porque tienes %s",
		"release.tba":             "fecha por anunciar",
		"deal.title":              "%s en mínimo histórico",
		"deal.text":               "%s (−%d %%, antes %s) en %s",
		"issue.opened.title":      "⚠ %s",
		"issue.resolved.title":    "✓ %s vuelve a funcionar",
		"keys.title":              "%d claves caducan en 30 días",
		"keys.title.one":          "Una clave caduca en 30 días",
		"keys.line":               "%s (%s): %s",
		"days.today":              "hoy",
		"days.one":                "en 1 día",
		"days.other":              "en %d días",
		"logins.title":            "%d accesos fallidos desde %s",
		"logins.text":             "Entre %s y %s. Si no has sido tú, revisa quién puede acceder a Game Vault.",
		"open":                    "Abrir en Game Vault",
		"open.page":               "Abrir la página",
		"test.title":              "Prueba de Game Vault",
		"test.text":               "Esta conexión funciona. Example Game y Another Game aparecerían así.",
		"kind.backups":            "Copias de seguridad",
		"kind.disk":               "Espacio en disco",

		"reason.provider_quota":          "Cupo mensual agotado; se renueva {{until}}",
		"reason.provider_rate_limited":   "Demasiadas peticiones; se reintenta en unos minutos",
		"reason.provider_unavailable":    "No responde; se reintenta en unos minutos",
		"reason.provider_settings":       "Rechaza su configuración: revísala en Proveedores",
		"reason.source_signed_out":       "Sesión cerrada: vuelve a iniciar sesión en Fuentes",
		"reason.source_sync_failed":      "El último escaneo falló",
		"reason.source_keepalive_failed": "No se pudo mantener la sesión activa",
		"reason.source_list_failed":      "No se pudieron leer las fuentes para comprobarlas",
		"reason.backup_failed":           "La última copia de seguridad falló: haz una ahora en Sistema → Copias de seguridad",
		"reason.backup_stale":            "Ninguna copia de seguridad en {{days}} días",
		"reason.disk_low":                "Solo quedan {{free}} libres ({{percent}} %) en el disco de los datos",
		"reason.disk_critical":           "Casi sin espacio: {{free}} libres en el disco de los datos",
		"reason.connector_paused":        "Las notificaciones están en pausa: {{reason}}",
		"date.on":                        "el %[2]d de %[1]s",
		"month.1":                        "enero",
		"month.2":                        "febrero",
		"month.3":                        "marzo",
		"month.4":                        "abril",
		"month.5":                        "mayo",
		"month.6":                        "junio",
		"month.7":                        "julio",
		"month.8":                        "agosto",
		"month.9":                        "septiembre",
		"month.10":                       "octubre",
		"month.11":                       "noviembre",
		"month.12":                       "diciembre",
	},
}

// fallbackLanguage is the language of a text missing in another.
const fallbackLanguage = "en"

// text returns the text of key in lang (English when lang lacks it, the key itself when no
// language has it), formatted with args.
func text(lang, key string, args ...any) string {
	s, ok := texts[lang][key]
	if !ok {
		s, ok = texts[fallbackLanguage][key]
	}

	if !ok {
		return key
	}

	if len(args) == 0 {
		return s
	}

	return fmt.Sprintf(s, args...)
}

// fillParams replaces the {{name}} placeholders of s with params; a placeholder without a value is
// left empty rather than shown to the user.
func fillParams(s string, params map[string]string) string {
	var b strings.Builder

	for {
		start := strings.Index(s, "{{")
		if start < 0 {
			break
		}

		end := strings.Index(s[start:], "}}")
		if end < 0 {
			break
		}

		b.WriteString(s[:start])
		b.WriteString(params[s[start+2:start+end]])
		s = s[start+end+2:]
	}

	b.WriteString(s)

	return b.String()
}
