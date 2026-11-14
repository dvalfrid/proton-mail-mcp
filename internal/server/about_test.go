package server

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dvalfrid/proton-mail-mcp/internal/config"
)

// about gives the version it was built with, and its list names every
// registered tool exactly once: a new tool must be added to the catalog.
func TestAboutListsEveryTool(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	var out AboutOutput
	e.callOK(t, "about", map[string]any{}, &out)
	if out.Version != "test" {
		t.Errorf("version = %q, want the one New was given", out.Version)
	}

	var listed []string
	for _, g := range out.Groups {
		for _, tl := range g.Tools {
			if tl.Description == "" {
				t.Errorf("%s has no description", tl.Name)
			}
			listed = append(listed, tl.Name)
		}
	}
	res, err := e.session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var registered []string
	for _, tool := range res.Tools {
		registered = append(registered, tool.Name)
	}
	slices.Sort(listed)
	slices.Sort(registered)
	if !slices.Equal(listed, registered) {
		t.Errorf("about lists %q,\nregistered are %q", listed, registered)
	}
}

// about shows the owner's options as New was given them (issue #158).
func TestAboutShowsTheSettings(t *testing.T) {
	t.Parallel()
	out := aboutOutput("v", []config.Option{{Name: "changes per run", Value: "200", Origin: config.FromDefault}})
	if len(out.Settings) != 1 || out.Settings[0].Origin != "default" || !strings.Contains(out.Note, "only the owner changes them") {
		t.Errorf("%+v", out)
	}
}
