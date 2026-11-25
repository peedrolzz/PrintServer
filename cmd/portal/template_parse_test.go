package main

import (
	"html/template"
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

func TestAllTemplatesParse(t *testing.T) {
	for _, page := range []string{"home", "login", "wifi", "wifi_scanning", "wifi_connecting", "diag", "bridge", "password", "password_wifi_saved", "first_run", "test_print", "error"} {
		if _, err := template.ParseFS(tplFS, "templates/base.html", "templates/"+page+".html"); err != nil {
			t.Errorf("%s: %v", page, err)
		}
	}
}

func TestEveryPostFormCarriesCSRF(t *testing.T) {
	formRe := regexp.MustCompile(`(?is)<form[^>]*method="post"[^>]*>(.*?)</form>`)
	csrfRe := regexp.MustCompile(`name="_csrf"`)
	err := fs.WalkDir(tplFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".html") {
			return err
		}
		b, err := fs.ReadFile(tplFS, path)
		if err != nil {
			return err
		}
		for _, m := range formRe.FindAllSubmatch(b, -1) {
			if !csrfRe.Match(m[1]) {
				t.Errorf("%s: <form method=post> missing hidden _csrf field", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
