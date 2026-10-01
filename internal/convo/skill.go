package convo

import (
	"path/filepath"
	"strings"

	"github.com/0xdeafcafe/rush/internal/agent"
	"github.com/0xdeafcafe/rush/internal/agent/tool"
)

// Recognize only dedicated skill calls and single-file reads. Mixed commands
// keep their ordinary output so the skill summary cannot hide other work.
func (d *drawer) skillInfo(st *Step) (name, description, path string) {
	switch {
	case st.Tool == "Skill" || st.Tool == "SlashCommand":
		in := readInput(st.Input)
		name = firstNonEmpty(in.str("skill"), in.str("command"))
	case st.kind() == tool.Read:
		path = st.in().Path
	case st.kind() == tool.Shell:
		sh := d.shellShape(st.in().Command)
		if sh.kind == "read" && !strings.Contains(sh.what, ", ") {
			path = sh.what
		}
	}
	if path != "" {
		if filepath.Base(path) != "SKILL.md" {
			return "", "", ""
		}
		name = filepath.Base(filepath.Dir(path))
	}
	if name == "" {
		return "", "", ""
	}
	if st.Status == OK {
		output := st.Output
		if st.kind() == tool.Shell {
			output = bashOut(st)
		}
		meta := agent.FrontMatter(strings.NewReader(strings.TrimSpace(unnumbered(output))))
		name = firstNonEmpty(meta["name"], name)
		description = meta["description"]
	}
	return oneLine(name), oneLine(description), path
}

func (d *drawer) skillStep(st *Step, ref string, indent int) bool {
	name, description, path := d.skillInfo(st)
	if name == "" {
		return false
	}
	state := "Loading skill"
	switch st.Status {
	case OK:
		state = "Loaded skill"
	case Failed:
		state = "Couldn't load skill"
	case Waiting:
		state = "Skill awaiting permission"
	}
	pad := d.spine() + strings.Repeat(" ", indent-1)
	d.worked = true
	d.add(ref, "", pad+d.statusMark(st)+" "+paint(cOrange, "✦")+" "+sub(state)+faint(" · ")+text(name), "")
	if description != "" {
		rows := wrap(description, max(12, d.cw-indent-4))
		for i, row := range rows {
			if i == 2 {
				break
			}
			if i == 1 && len(rows) > 2 {
				row += "…"
			}
			d.add(ref, "", pad+"    "+dim(row), "")
		}
	}
	open := d.o.Verbose
	if v, ok := d.o.Open[ref]; ok {
		open = v
	}
	if open {
		if path != "" {
			d.add(ref, "", pad+"    "+d.fileLink(path, faint(d.rel(path))), "")
		}
		d.body(st, indent+4)
	} else if st.Status == Failed {
		d.errorLine(st, indent+4, ref)
	}
	d.denial(st, indent+2)
	return true
}
