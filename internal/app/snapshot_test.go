package app

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

func TestSnapshot(t *testing.T) {
	dir := os.Getenv("DISPLAYEXT_SNAPSHOT_DIR")
	if dir == "" {
		t.Skip("set DISPLAYEXT_SNAPSHOT_DIR to write screenshots of the settings window")
	}
	shots := []struct {
		name    string
		dark    bool
		mounted bool
		general bool
		picker  bool
	}{
		{"settings-light", false, true, false, false},
		{"settings-dark", true, true, false, false},
		{"settings-unmounted", false, false, false, false},
		{"settings-picker", false, false, false, true},
		{"general-light", false, false, true, false},
		{"general-dark", true, false, true, false},
	}
	defer i18n.SetLocaleFunc(func() string { return "" })
	for _, lang := range []string{"en", "zh-Hans"} {
		i18n.SetLocaleFunc(func() string { return lang })
		for _, shot := range shots {
			f := newFake()
			f.wanted[f.displays[0].ID] = shot.mounted
			s := NewSettings(f)
			if shot.general {
				s.showGeneral()
			}
			s.pickerOpen = shot.picker
			tt := ui.NewTesterWith(s.View, 820, 830, ui.TesterOptions{Dark: shot.dark})
			tt.SetLocale(lang)
			tt.SetScale(2)
			tt.SetTitleBar(ui.TitleBar{Height: 52, Left: 78, Bottom: 28})
			tt.Frame()
			name := shot.name + ".png"
			if lang != "en" {
				name = shot.name + "." + lang + ".png"
			}
			out, err := os.Create(filepath.Clean(filepath.Join(dir, name)))
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(out, tt.Image()); err != nil {
				t.Fatal(err)
			}
			if err := out.Close(); err != nil {
				t.Fatal(err)
			}
		}
	}
}
