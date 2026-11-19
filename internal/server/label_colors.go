package server

import (
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
	"github.com/dvalfrid/proton-mail-mcp/internal/mail"
)

// labelColors are the colors the owner gave their labels (issue #209), by
// label name in lower case, as emoji. Bridge doesn't send Proton's label
// colors, so they come from the owner's settings.
type labelColors map[string]string

// newLabelColors turns the owner's label → color name settings into emoji.
func newLabelColors(byName map[string]string) labelColors {
	lc := labelColors{}
	for name, color := range byName {
		if e := config.ColorEmoji(color); e != "" {
			lc[strings.ToLower(name)] = e
		}
	}
	return lc
}

// labelsPrefix is where Bridge shows the owner's labels as folders.
const labelsPrefix = "Labels/"

// of returns the emoji of each label on msgs that has a color, by the name
// the messages carry; nil if none has one.
func (lc labelColors) of(msgs []mail.Summary) map[string]string {
	var out map[string]string
	for _, m := range msgs {
		for _, l := range m.Labels {
			if e, ok := lc[strings.ToLower(l)]; ok {
				if out == nil {
					out = map[string]string{}
				}
				out[l] = e
			}
		}
	}
	return out
}

// colorFolders sets the color of each label in folders that has one.
func (lc labelColors) colorFolders(folders []mail.Folder) {
	for i, f := range folders {
		if name, ok := strings.CutPrefix(f.Name, labelsPrefix); ok && f.Role == "" {
			folders[i].Color = lc[strings.ToLower(name)]
		}
	}
}

// labelColorsNote tells the assistant how to show colored labels.
const labelColorsNote = " label_colors gives the color the owner picked for a label, as an emoji: put it right before the label's name, e.g. Invoice 🔵Work 🟢Family. A label not in label_colors has no color: show it as a plain tag."
