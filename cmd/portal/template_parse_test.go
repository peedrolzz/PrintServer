package main

import (
	"html/template"
	"testing"
)

func TestAllTemplatesParse(t *testing.T) {
	for _, page := range []string{"home", "login", "wifi", "wifi_scanning", "wifi_connecting", "diag", "bridge", "password", "password_wifi_saved", "first_run", "test_print"} {
		if _, err := template.ParseFS(tplFS, "templates/base.html", "templates/"+page+".html"); err != nil {
			t.Errorf("%s: %v", page, err)
		}
	}
}
