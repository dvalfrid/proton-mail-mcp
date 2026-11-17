package wizard

import (
	"context"

	"github.com/dvalfrid/proton-mail-mcp/internal/check"
	"github.com/dvalfrid/proton-mail-mcp/internal/help"
)

// StatusResult is the Status page: the steps of `proton-mail-mcp check`, run
// by the same code on settings loaded the same way.
type StatusResult struct {
	Failed bool         `json:"failed"`
	Steps  []StatusStep `json:"steps"`
}

// StatusStep is one step, as the page shows it.
type StatusStep struct {
	Name   string `json:"name"`
	Title  string `json:"title"`
	Status string `json:"status"` // "ok", "fail" or "advice"
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
	Help   string `json:"help,omitempty"` // a troubleshooting topic, for openLink "help:<topic>"
}

var statusNames = map[check.Status]string{check.OK: "ok", check.Fail: "fail", check.Advice: "advice"}

// runCheck runs the check for the server this window sets up: its .env
// file, the settings in o.Dir and the password in the credential store. The
// password stays in this process; the steps never contain it.
func (b *Backend) runCheck(ctx context.Context) StatusResult {
	steps := check.Diagnose(ctx, check.EnvFile(b.o.Exe), b.o.Dir, b.o.Store.Get)
	return statusResult(steps)
}

func statusResult(steps []check.Step) StatusResult {
	r := StatusResult{Failed: check.Failed(steps), Steps: make([]StatusStep, 0, len(steps))}
	for _, s := range steps {
		st := StatusStep{Name: s.Name, Title: s.Title, Status: statusNames[s.Status], Detail: s.Detail}
		if s.Status != check.OK {
			st.Hint = s.Hint
			if s.Help != help.Troubleshoot {
				st.Help = string(s.Help)
			}
		}
		r.Steps = append(r.Steps, st)
	}
	return r
}
