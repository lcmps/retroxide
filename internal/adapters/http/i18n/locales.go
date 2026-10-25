package i18n

import (
	"embed"
	"fmt"
)

//go:embed en.json pt-BR.json
var catalogs embed.FS

func Catalogs() (map[string][]byte, error) {
	locales := map[string]string{"en": "en.json", "pt-BR": "pt-BR.json"}
	result := make(map[string][]byte, len(locales))
	for locale, filename := range locales {
		content, err := catalogs.ReadFile(filename)
		if err != nil {
			return nil, fmt.Errorf("reading embedded locale %s: %w", locale, err)
		}
		result[locale] = content
	}
	return result, nil
}
