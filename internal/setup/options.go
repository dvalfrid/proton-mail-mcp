package setup

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// SettingsCommand is `proton-mail-mcp settings`: the owner's options shown
// and changed in a terminal, for macOS, Linux and Windows without the
// window (issue #180). It saves through config.ApplyOptions, as the
// Settings page does, and never touches the password or the certificate.
// Only the owner runs it; no MCP tool can (TestNoToolWritesSettings).
type SettingsCommand struct {
	Dir     string    // the settings folder (config.json)
	EnvFile string    // the .env the server reads
	In      io.Reader // the owner's answer when sending is switched on
	Out     io.Writer
}

// option is one option by its config.json name.
type option struct {
	name, key, about string
}

var settingsOptions = []option{
	{"allow_remote_images", config.EnvAllowRemoteImages, "on or off: fetch_image fetches an image a message links to, when you ask"},
	{"attachment_dir", config.EnvAttachmentDir, "a full path, or default: where save_attachment saves"},
	{"outbox_dir", config.EnvOutboxDir, "a full path, or default: the only folder drafts attach files from"},
	{"max_changes", config.EnvMaxChanges, fmt.Sprintf("1 to %d: messages changed and drafts written per run", config.MaxMaxChanges)},
	{"allow_send", config.EnvAllowSend, "on or off: send_draft sends a draft you have seen, after asking you"},
	{"label_color", config.EnvLabelColors, "<label> <color>, or <label> none: the color Claude shows the label in (" + colorList() + ")"},
}

// colorList names the colors a label can have.
func colorList() string {
	names := make([]string, len(config.Colors))
	for i, c := range config.Colors {
		names[i] = c.Name
	}
	return strings.Join(names, ", ")
}

// errUsage is a command the settings command doesn't know.
var errUsage = errors.New("usage: proton-mail-mcp settings [set <option> <value>], or settings set label_color <label> <color>")

// Run shows the options (no args) or changes one ("set", name, value).
func (c SettingsCommand) Run(args []string) error {
	dotenv, err := config.ReadDotEnv(c.EnvFile)
	if err != nil {
		return err
	}
	file := config.SettingsFile(c.Dir)
	saved, err := config.ReadSettings(file)
	if err != nil {
		return fmt.Errorf("the settings could not be read (%w); run proton-mail-mcp setup first", err)
	}
	if len(args) == 0 {
		c.show(dotenv, saved)
		return nil
	}
	switch len(args) {
	case 4: // set label_color <label> <color>
		if a := [4]string(args); a[0] == "set" && a[1] == "label_color" {
			return c.setLabelColor(file, dotenv, saved, a[2], a[3])
		}
	case 3: // set <option> <value>
		if a := [3]string(args); a[0] == "set" && a[1] != "label_color" {
			return c.set(file, dotenv, saved, a[1], a[2])
		}
	}
	return errUsage
}

func (c SettingsCommand) show(dotenv map[string]string, saved config.Settings) {
	v := saved.OptionValues()
	shown := map[string]string{
		"allow_remote_images": onOff(v.AllowRemoteImages), "attachment_dir": orDefault(v.AttachmentDir),
		"outbox_dir": orDefault(v.OutboxDir), "max_changes": strconv.Itoa(v.MaxChanges), "allow_send": onOff(v.AllowSend),
		"label_color": config.FormatLabelColors(v.LabelColors),
	}
	lookup := config.Lookup(dotenv)
	for _, o := range settingsOptions {
		origin := config.Origin(o.key, dotenv, saved)
		value := shown[o.name]
		if origin == config.FromEnvironment || origin == config.FromDotEnv {
			value, _ = lookup(o.key)
			origin += ", which overrides config.json"
		}
		_, _ = fmt.Fprintf(c.Out, "%-20s %s  (%s)\n", o.name, value, origin) // a failed write to the terminal can't be reported
	}
	_, _ = fmt.Fprintln(c.Out, "\nChange one with: proton-mail-mcp settings set <option> <value>")
	for _, o := range settingsOptions {
		_, _ = fmt.Fprintf(c.Out, "  %-20s %s\n", o.name, o.about)
	}
}

func (c SettingsCommand) set(file string, dotenv map[string]string, saved config.Settings, name, value string) error {
	var opt *option
	for i := range settingsOptions {
		if settingsOptions[i].name == name {
			opt = &settingsOptions[i]
		}
	}
	if opt == nil || opt.name == "label_color" {
		return fmt.Errorf("no option %q; the options are allow_remote_images, attachment_dir, outbox_dir, max_changes, allow_send and label_color", name)
	}
	locked := config.Locked(dotenv, saved)
	if locked[opt.key] {
		return fmt.Errorf("%s is set in %s, which overrides config.json: change it there", name, config.Origin(opt.key, dotenv, saved))
	}
	v := saved.OptionValues()
	var err error
	switch name {
	case "allow_remote_images":
		v.AllowRemoteImages, err = parseOnOff(value)
	case "allow_send":
		v.AllowSend, err = parseOnOff(value)
	case "attachment_dir":
		v.AttachmentDir = folderValue(value)
	case "outbox_dir":
		v.OutboxDir = folderValue(value)
	case "max_changes":
		if v.MaxChanges, err = strconv.Atoi(value); err != nil {
			err = fmt.Errorf("max_changes must be a whole number from 1 to %d", config.MaxMaxChanges)
		}
	}
	if err != nil {
		return err
	}
	return c.save(file, saved, v, locked, name)
}

// setLabelColor gives label a color, or takes its color away ("none").
func (c SettingsCommand) setLabelColor(file string, dotenv map[string]string, saved config.Settings, label, color string) error {
	locked := config.Locked(dotenv, saved)
	if locked[config.EnvLabelColors] {
		return fmt.Errorf("label colors are set in %s (%s), which overrides config.json: change them there", config.Origin(config.EnvLabelColors, dotenv, saved), config.EnvLabelColors)
	}
	label = strings.TrimSpace(label)
	v := saved.OptionValues()
	colors := map[string]string{}
	for name, c := range v.LabelColors {
		if !strings.EqualFold(name, label) { // the new color replaces the old, whatever its case
			colors[name] = c
		}
	}
	if !strings.EqualFold(color, "none") {
		colors[label] = color
	}
	v.LabelColors = colors
	return c.save(file, saved, v, locked, "the color of "+label)
}

// save checks v, writes it into the settings file and says so.
func (c SettingsCommand) save(file string, saved config.Settings, v config.OptionValues, locked map[string]bool, what string) error {
	updated, err := config.ApplyOptions(saved, v, locked, false)
	if errors.Is(err, config.ErrConfirmSend) {
		if !c.confirmSend() {
			return errors.New("sending stays off")
		}
		updated, err = config.ApplyOptions(saved, v, locked, true)
	}
	if err != nil {
		return err
	}
	if err := config.WriteSettings(file, updated); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(c.Out, "Saved %s. Restart Claude to use it.\n", what) // a failed write to the terminal can't be reported
	return nil
}

// confirmSend asks the owner, in the terminal, before sending is switched
// on, as the Settings page does with its tick-box.
func (c SettingsCommand) confirmSend() bool {
	_, _ = fmt.Fprint(c.Out, "Claude can then send email in your name: a draft you have seen, after asking you, at most 10 per session. Sent mail can't be taken back.\n"+
		"Type yes to switch sending on: ")
	line, _ := bufio.NewReader(c.In).ReadString('\n') // no answer is a no
	return strings.EqualFold(strings.TrimSpace(line), "yes")
}

func parseOnOff(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "1", "true", "yes":
		return true, nil
	case "off", "0", "false", "no":
		return false, nil
	}
	return false, fmt.Errorf("%q: use on or off", s)
}

func folderValue(s string) string {
	if strings.EqualFold(s, "default") {
		return ""
	}
	return s
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func orDefault(dir string) string {
	if dir == "" {
		return "default"
	}
	return dir
}
