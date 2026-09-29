package agent

import (
	"path/filepath"
	"strings"
)

// Media is what a model reads besides text, a bit for each kind.
type Media uint8

const (
	MediaImage Media = 1 << iota
	MediaPDF
	MediaAudio
	MediaVideo
)

// String is m's first kind as a message says it: "images".
func (m Media) String() string {
	switch {
	case m&MediaImage != 0:
		return "images"
	case m&MediaPDF != 0:
		return "PDFs"
	case m&MediaAudio != 0:
		return "audio"
	case m&MediaVideo != 0:
		return "video"
	}
	return "text"
}

// MediaReader is an agent that knows what its models read besides text.
type MediaReader interface {
	// Reads is what model takes; false when the agent doesn't know it.
	Reads(model string) (Media, bool)
}

// mediaExt are the files a model reads as other than text, by extension.
var mediaExt = map[string]Media{
	".png": MediaImage, ".jpg": MediaImage, ".jpeg": MediaImage, ".gif": MediaImage, ".webp": MediaImage,
	".pdf": MediaPDF,
	".mp3": MediaAudio, ".wav": MediaAudio, ".m4a": MediaAudio, ".ogg": MediaAudio, ".flac": MediaAudio,
	".mp4": MediaVideo, ".mov": MediaVideo, ".webm": MediaVideo, ".mkv": MediaVideo, ".avi": MediaVideo,
}

// MediaOf is what kind of file path is by its name; 0 for anything else.
func MediaOf(path string) Media { return mediaExt[strings.ToLower(filepath.Ext(path))] }

// Unreadable is why agent k's model can't be sent files: "Codex can't
// take images", "gemma3:4b can't read images". An empty model is the
// agent's default. It's "" when every file can go, and when the agent
// doesn't know the model.
// ponytail: an unknown model is let through, so a gap in an adapter's
// table never blocks a send; the agent says no itself if it must.
func Unreadable(k Kind, model string, files []string) string {
	var need Media
	for _, f := range files {
		need |= MediaOf(f)
	}
	if need == 0 {
		return ""
	}
	if need&MediaImage != 0 && !Supports(k, FeatureImages) {
		return nameOf(k) + " can't take images"
	}
	r, ok := As[MediaReader](k)
	if !ok {
		return ""
	}
	if d, ok := As[DefaultModeler](k); ok && model == "" {
		model = d.DefaultModel()
	}
	has, ok := r.Reads(model)
	if !ok || need&^has == 0 {
		return ""
	}
	return ModelName(k, model) + " can't read " + (need &^ has).String()
}

func nameOf(k Kind) string {
	if a, ok := Get(k); ok {
		return a.Name()
	}
	return string(k)
}
