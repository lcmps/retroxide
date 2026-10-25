package i18n

import (
	"encoding/json"
	"testing"
)

func TestCatalogsContainsValidCatalogs(t *testing.T) {
	catalogs, err := Catalogs()
	if err != nil {
		t.Fatal(err)
	}
	for _, locale := range []string{"en", "pt-BR"} {
		content, ok := catalogs[locale]
		if !ok {
			t.Fatalf("catalog %q is missing", locale)
		}
		var entries map[string]string
		if err := json.Unmarshal(content, &entries); err != nil {
			t.Fatalf("catalog %q is invalid JSON: %v", locale, err)
		}
		if entries["common.actions.cancel"] == "" {
			t.Fatalf("catalog %q is missing common.actions.cancel", locale)
		}
	}
}
