package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/0xdeafcafe/agtop/internal/keymap"
	"github.com/0xdeafcafe/agtop/internal/plugin"
)

// Settings, Plugins: what each plugin taking part in agtop's screen may do
// there, its settings, and its commands' keys. agtop keeps the values, and
// the broker hands them to the plugin.

var uiCapWords = map[string]string{
	plugin.UIEvents:    "hears what happens",
	plugin.UIInput:     "sees what you type",
	plugin.UIIntercept: "is asked before a message goes",
	plugin.UIOverview:  "adds to overviews",
	plugin.UINotify:    "shows notices",
	plugin.UISend:      "sends to sessions",
}

func (m *Model) pluginSections() []section {
	var ps []plugin.UIPlugin
	if m.hooks != nil {
		ps = m.hooks.State().Plugins
	}
	if len(ps) == 0 {
		return []section{{title: "Plugins", rows: []setting{{
			label: "none",
			line: func(w int) string {
				return dim("No plugin takes part in agtop's screen. ") + faint("agtop plugin list shows what's installed; approve one with agtop plugin approve <name>.")
			},
			what: "A plugin whose manifest asks for \"ui\", \"commands\" or \"settings\" shows here once approved and running.",
		}}}}
	}
	var out []section
	for _, p := range ps {
		sec := section{title: p.Name, note: capWords(p)}
		for _, spec := range p.Settings {
			sec.rows = append(sec.rows, m.pluginSetting(p, spec))
		}
		for _, c := range p.Commands {
			id := keymap.PluginID(p.Name, c.Name)
			sec.rows = append(sec.rows, setting{
				label: "#" + c.Name,
				line: func(w int) string {
					k := m.keyMap().KeyText(id)
					if k == "" {
						k = "no key"
					}
					return fit("command "+c.Name, 32) + paint(cText, k) + faint("  "+c.Description)
				},
				what: c.Description + " Settings, Keys changes its key.",
				key: func(s string) (tea.Cmd, bool) {
					if s == "enter" {
						m.setSettingsPage(pageKeys)
						for i, a := range m.keyRows() {
							if a.ID == id {
								m.dialog.cursor = i
							}
						}
						return nil, true
					}
					return nil, false
				},
				keys: []string{"enter", "its key"},
			})
		}
		if p.Skipped != "" {
			sec.rows = append(sec.rows, setting{label: "skipped", line: func(int) string { return paint(cYellow, "intercepts skipped: "+p.Skipped) }, what: "It stopped answering in time, so messages go without asking it until it restarts."})
		}
		if len(sec.rows) == 0 {
			sec.rows = append(sec.rows, setting{label: "nothing", line: func(int) string { return faint("nothing to set") }, what: "It has no settings or commands; it " + capWords(p) + "."})
		}
		out = append(out, sec)
	}
	return out
}

func capWords(p plugin.UIPlugin) string {
	w := make([]string, 0, len(p.UI))
	for _, c := range p.UI {
		w = append(w, uiCapWords[c])
	}
	if len(w) == 0 {
		return "commands and settings only"
	}
	return joinAnd(w)
}

func joinAnd(xs []string) string {
	switch len(xs) {
	case 0:
		return ""
	case 1:
		return xs[0]
	}
	return strings.Join(xs[:len(xs)-1], ", ") + " and " + xs[len(xs)-1]
}

// pluginSetting is one of a plugin's settings as a row; changing it asks
// the broker, off the UI.
func (m *Model) pluginSetting(p plugin.UIPlugin, spec plugin.SettingSpec) setting {
	v := p.Values[spec.Key]
	st := setting{label: spec.Title, value: v, what: spec.Description, unset: spec.Default, means: map[string]string{}}
	switch spec.Type {
	case "bool":
		st.choices = []string{"true", "false"}
		st.means = map[string]string{"true": "on", "false": "off"}
	case "choice":
		st.choices = spec.Choices
	case "text":
		st.typed = true
	}
	name, key := p.Name, spec.Key
	st.run = func(v string) tea.Cmd {
		return m.hooks.SetSetting(name, key, v, func(err error) tea.Msg {
			return sheetMsg{apply: func(m *Model) tea.Cmd {
				if err != nil {
					m.flash(name+": "+err.Error(), true)
				}
				return nil
			}}
		})
	}
	return st
}
