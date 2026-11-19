package config

import (
	"strings"
	"testing"
)

// Label colors come from config.json as a map and from .env or the
// environment as JSON; both are checked the same way (issue #209).
func TestLabelColors(t *testing.T) {
	base := map[string]string{EnvPassword: "secret-for-tests"}
	saved := Settings{User: "alice@example.com", CertFile: "c.pem", LabelColors: map[string]string{"Work": "blue", "Family": "green"}}
	cfg, err := Load(Chain(FromMap(saved.Values()), FromMap(base)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LabelColors["Work"] != "blue" || cfg.LabelColors["Family"] != "green" || len(cfg.LabelColors) != 2 {
		t.Errorf("from config.json: %v", cfg.LabelColors)
	}
	if got := FormatLabelColors(cfg.LabelColors); got != "Family=green, Work=blue" {
		t.Errorf("shown as %q", got)
	}

	for v, want := range map[string]string{
		`not json`:                     "JSON object",
		`{"Work":"pink"}`:              "not a color",
		`{"":"blue"}`:                  "label's name",
		`{"Work":"blue","work":"red"}`: "more than one color",
		`{"Two\nlines":"blue"}`:        "not a label name",
		`{"` + strings.Repeat("x", 101) + `":"red"}`: "not a label name",
	} {
		_, err := Load(Chain(FromMap(map[string]string{EnvLabelColors: v}), FromMap(saved.Values()), FromMap(base)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s=%q: err = %v, want %q", EnvLabelColors, v, err, want)
		}
	}

	cfg, err = Load(Chain(FromMap(map[string]string{EnvLabelColors: ` {" Work ":" BLUE "} `}), FromMap(base), FromMap(saved.Values())))
	if err != nil || cfg.LabelColors["Work"] != "blue" {
		t.Errorf("cleaned: %v, %v", cfg.LabelColors, err)
	}
}

// The Settings page and `settings set` save label colors through
// ApplyOptions, which checks them and leaves them alone when .env sets them.
func TestApplyLabelColors(t *testing.T) {
	t.Parallel()
	saved := Settings{User: "alice@example.com", LabelColors: map[string]string{"Old": "red"}}
	v := saved.OptionValues()
	v.LabelColors = map[string]string{"Work": "Blue"}
	got, err := ApplyOptions(saved, v, map[string]bool{}, false)
	if err != nil || len(got.LabelColors) != 1 || got.LabelColors["Work"] != "blue" {
		t.Errorf("saved %v, %v", got.LabelColors, err)
	}
	got, _ = ApplyOptions(saved, v, map[string]bool{EnvLabelColors: true}, false)
	if got.LabelColors["Old"] != "red" {
		t.Errorf("locked: %v", got.LabelColors)
	}
	v.LabelColors = map[string]string{"Work": "teal"}
	if _, err := ApplyOptions(saved, v, map[string]bool{}, false); err == nil {
		t.Error("teal was accepted")
	}
	v.LabelColors = nil
	if got, _ := ApplyOptions(saved, v, map[string]bool{}, false); got.LabelColors != nil {
		t.Errorf("cleared: %v", got.LabelColors)
	}
}
