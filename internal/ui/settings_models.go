package ui

import "github.com/0xdeafcafe/rush/internal/agent"

// Provider catalogs may read homes or contact local servers; never do that from
// rendering or a key handler. Settings populates automatically in the background.
func readSettingsModels() map[string][]agent.Choice {
	out := map[string][]agent.Choice{}
	for _, ad := range agent.InstalledAll() {
		if agent.CurrentKind(ad.Kind()) != ad.Kind() {
			continue
		}
		if lister, ok := ad.(agent.ModelLister); ok {
			if profiles := ad.Profiles(); len(profiles) > 0 {
				out[string(ad.Kind())] = lister.ListModels(profiles[0])
			}
		}
	}
	return out
}
func (m *Model) applySettingsModels() {
	d := m.dialog
	if d.modelsRead == nil || d.modelsApplied {
		return
	}
	if found, ready := d.modelsRead.take(); ready {
		d.modelsApplied = true
		if m.listed == nil {
			m.listed = map[string][]agent.Choice{}
		}
		for k, v := range found {
			m.listed[k] = v
		}
	}
}
