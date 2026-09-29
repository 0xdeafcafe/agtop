package agent

import "testing"

type readerAdapter struct {
	progAdapter
	images bool
}

func (r readerAdapter) Features() map[Feature]Support {
	if r.images {
		return map[Feature]Support{FeatureImages: Yes}
	}
	return nil
}

func (readerAdapter) DefaultModel() string { return "seer" }

// Reads knows "seer", which reads images, and "blind", which reads text.
func (readerAdapter) Reads(model string) (Media, bool) {
	switch model {
	case "seer":
		return MediaImage | MediaPDF, true
	case "blind":
		return 0, true
	}
	return 0, false
}

// A file goes only to a model that reads its kind; an agent without
// images, or a model that can't, says so, and a model no one knows lets
// it through.
func TestUnreadable(t *testing.T) {
	Register(readerAdapter{progAdapter{kind: "test-reader"}, true})
	Register(readerAdapter{progAdapter{kind: "test-noimages"}, false})
	defer func() {
		mu.Lock()
		delete(adapters, "test-reader")
		delete(adapters, "test-noimages")
		mu.Unlock()
	}()
	for _, c := range []struct {
		k           Kind
		model, file string
		want        string
	}{
		{"test-reader", "seer", "a.PNG", ""},
		{"test-reader", "", "a.png", ""}, // its default, seer
		{"test-reader", "seer", "a.mp4", "seer can't read video"},
		{"test-reader", "blind", "a.png", "blind can't read images"},
		{"test-reader", "blind", "notes.txt", ""},
		{"test-reader", "new-model", "a.wav", ""},
		{"test-noimages", "seer", "a.png", "test-noimages can't take images"},
	} {
		if got := Unreadable(c.k, c.model, []string{c.file}); got != c.want {
			t.Errorf("Unreadable(%s, %q, %s) = %q, want %q", c.k, c.model, c.file, got, c.want)
		}
	}
}
