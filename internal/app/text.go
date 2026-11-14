package app

import (
	"displayext/internal/config"
	"displayext/internal/uudetect"
	"displayext/locales"

	"github.com/egoist/mygo/i18n"
)

const customRatio = "custom"

var languages = []string{"", "en", "zh-Hans"}

func languageLabel(tag string) string {
	if tag == "" {
		return locales.LanguageSystem()
	}
	return i18n.Autonym(tag)
}

func uuLabel(st uudetect.State) string {
	switch st {
	case uudetect.Connected:
		return locales.UuConnected()
	case uudetect.Idle:
		return locales.UuIdle()
	}
	return locales.UuNotRunning()
}

func aspectNote(a config.Aspect) string {
	switch a {
	case config.Aspect{W: 4, H: 3}:
		return locales.Aspect4x3()
	case config.Aspect{W: 16, H: 10}:
		return locales.Aspect16x10()
	case config.Aspect{W: 16, H: 9}:
		return locales.Aspect16x9()
	case config.Aspect{W: 3, H: 2}:
		return locales.Aspect3x2()
	case config.Aspect{W: 21, H: 9}:
		return locales.Aspect21x9()
	}
	return ""
}

func ratioLabel(a config.Aspect) string {
	if note := aspectNote(a); note != "" {
		return a.String() + " — " + note
	}
	return a.String()
}

func ratioKeyLabel(key string) string {
	if key == customRatio {
		return locales.RatioCustom()
	}
	for _, a := range config.Presets {
		if a.String() == key {
			return ratioLabel(a)
		}
	}
	return key
}

func ratioOptions() []string {
	out := make([]string, 0, len(config.Presets)+1)
	for _, a := range config.Presets {
		out = append(out, ratioLabel(a))
	}
	return append(out, locales.RatioCustom())
}

func ratioKeyOf(label string) (string, bool) {
	if label == locales.RatioCustom() {
		return customRatio, true
	}
	for _, a := range config.Presets {
		if ratioLabel(a) == label {
			return a.String(), true
		}
	}
	return "", false
}

func languageOptions() []string {
	out := make([]string, len(languages))
	for i, tag := range languages {
		out[i] = languageLabel(tag)
	}
	return out
}

func languageOf(label string) (string, bool) {
	for _, tag := range languages {
		if languageLabel(tag) == label {
			return tag, true
		}
	}
	return "", false
}

func problemText(d config.Display) string {
	switch d.Check() {
	case config.NoProblem:
		return ""
	case config.BadName:
		return locales.ProblemName()
	case config.BadAspect:
		return locales.ProblemAspect(config.MaxRatio)
	case config.BadSize:
		return locales.ProblemSize(config.MinPoints, config.MinShortPoints, config.MaxPoints)
	case config.BadRefresh:
		return locales.ProblemRefresh(config.MinRefresh, config.MaxRefresh)
	}
	return locales.ProblemOther()
}

func triggerHelp(t config.Trigger) string {
	if t == config.TriggerUU {
		return locales.HelpTriggerUU()
	}
	return locales.HelpTriggerManual()
}
