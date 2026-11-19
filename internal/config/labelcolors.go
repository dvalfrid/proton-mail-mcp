package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// EnvLabelColors gives the owner's labels a color each (issue #209): a JSON
// object from label name to color, e.g. {"Work":"blue","Family":"green"}.
// Bridge doesn't send Proton's label colors, so the owner picks them here.
const EnvLabelColors = "PROTON_LABEL_COLORS"

// Color is one color a label can have: the colored circles chat clients
// can show. Proton has more shades (pink, for one); the owner picks the
// nearest.
type Color struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"`
}

// Colors are the colors a label can have, in the order the Settings page
// shows them.
var Colors = []Color{
	{"red", "🔴"}, {"orange", "🟠"}, {"yellow", "🟡"}, {"green", "🟢"}, {"blue", "🔵"},
	{"purple", "🟣"}, {"brown", "🟤"}, {"black", "⚫"}, {"white", "⚪"},
}

// Limits on the label colors, so a mistyped setting can't grow without end.
const (
	MaxLabelColors   = 200
	maxLabelNameRune = 100
)

// ColorEmoji is the emoji of the color named name ("" if there is none).
func ColorEmoji(name string) string {
	for _, c := range Colors {
		if c.Name == name {
			return c.Emoji
		}
	}
	return ""
}

func colorNames() string {
	names := make([]string, len(Colors))
	for i, c := range Colors {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// CheckLabelColors checks the owner's label colors and returns them cleaned:
// names trimmed, colors in lower case. Nil if there are none. Label names
// are compared without regard to case, as Proton does.
func CheckLabelColors(in map[string]string) (map[string]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > MaxLabelColors {
		return nil, fmt.Errorf("at most %d label colors", MaxLabelColors)
	}
	out := make(map[string]string, len(in))
	seen := map[string]bool{}
	for name, color := range in {
		name = strings.TrimSpace(name)
		color = strings.ToLower(strings.TrimSpace(color))
		switch {
		case name == "":
			return nil, errors.New("a label color needs the label's name")
		case utf8.RuneCountInString(name) > maxLabelNameRune || strings.ContainsFunc(name, unicode.IsControl):
			return nil, fmt.Errorf("%q is not a label name: one line of at most %d characters", name, maxLabelNameRune)
		case ColorEmoji(color) == "":
			return nil, fmt.Errorf("%q is not a color for label %q: use one of %s", color, name, colorNames())
		case seen[strings.ToLower(name)]:
			return nil, fmt.Errorf("label %q has more than one color", name)
		}
		seen[strings.ToLower(name)] = true
		out[name] = color
	}
	return out, nil
}

// parseLabelColors reads EnvLabelColors' value.
func parseLabelColors(v string) (map[string]string, error) {
	var m map[string]string
	if err := json.Unmarshal([]byte(v), &m); err != nil {
		return nil, fmt.Errorf(`%s: give a JSON object from label to color, like {"Work":"blue"}`, EnvLabelColors)
	}
	m, err := CheckLabelColors(m)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvLabelColors, err)
	}
	return m, nil
}

// FormatLabelColors shows label colors in one line, by label name:
// "Family=green, Work=blue", or "none".
func FormatLabelColors(m map[string]string) string {
	if len(m) == 0 {
		return "none"
	}
	names := make([]string, 0, len(m))
	for name := range m {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return strings.ToLower(names[i]) < strings.ToLower(names[j]) })
	parts := make([]string, len(names))
	for i, name := range names {
		parts[i] = name + "=" + m[name]
	}
	return strings.Join(parts, ", ")
}
