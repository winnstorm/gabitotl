package frontend

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/winnstorm/gabitotl/internal/installer/ui"
	"github.com/winnstorm/gabitotl/internal/persona"
)

var (
	// t('key') calls, and every quoted gui.* key (tables of keys, data
	// attributes), so keys looked up indirectly are checked too.
	callRe    = regexp.MustCompile(`(?:^|[^\w$.])t\(\s*['"]([A-Za-z][\w.-]*)['"]`)
	literalRe = regexp.MustCompile(`['"](gui\.[a-z0-9][a-z0-9.-]*[a-z0-9])['"]`)
)

// usedKeys returns every catalogue key the embedded pages refer to.
func usedKeys(t *testing.T) []string {
	t.Helper()
	seen := map[string]bool{}
	err := fs.WalkDir(Assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !(strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".html")) {
			return err
		}
		src, err := fs.ReadFile(Assets, path)
		if err != nil {
			return err
		}
		for _, m := range callRe.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
		}
		for _, m := range literalRe.FindAllStringSubmatch(string(src), -1) {
			seen[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestEveryKeyUsedByThePagesExistsInBothCatalogues(t *testing.T) {
	keys := usedKeys(t)
	if len(keys) < 50 {
		t.Fatalf("found only %d keys in the pages; the scan is probably broken", len(keys))
	}
	for lang, name := range map[persona.Lang]string{persona.LangES: "es-AR", persona.LangEN: "en"} {
		m := ui.For(lang)
		for _, k := range keys {
			if _, ok := m[k]; !ok {
				t.Errorf("key %q is used by the frontend but missing from the %s catalogue", k, name)
			}
		}
	}
}

func TestEmbeddedPagesAreComplete(t *testing.T) {
	for _, f := range []string{"index.html", "app.css", "app.js", "i18n.js", "wizard.js"} {
		if b, err := fs.ReadFile(Assets, f); err != nil || len(b) == 0 {
			t.Errorf("%s is not embedded or empty: %v", f, err)
		}
	}
}
