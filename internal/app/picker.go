package app

import (
	"strings"

	"displayext/internal/config"
	"displayext/internal/devices"
	"displayext/locales"

	"github.com/egoist/mygo/ui"
)

func (s *Settings) deviceLabel() (string, *ui.SVG) {
	if m, ok := s.model(); ok {
		return modelTitle(m), kindIcon(m.Kind)
	}
	if s.ratioKey == customRatio {
		return locales.RatioCustom(), iconRatio
	}
	return ratioKeyLabel(s.ratioKey), iconDisplay
}

func kindIcon(k devices.Kind) *ui.SVG {
	if k == devices.Tablet {
		return iconTablet
	}
	return iconPhone
}

func (s *Settings) devicePicker(c *ui.Context, p palette) {
	t := c.Theme()
	label, icon := s.deviceLabel()
	anchor := ui.Button(c.Key("device"), "").Width(240).Label(locales.FieldDevice()).Justify(ui.Start).Gap(7).
		HasPopup(ui.PopupDialog).Expanded(s.pickerOpen).
		OnClick(func() { s.pickerOpen, s.query, s.reveal = !s.pickerOpen, "", true }).Children(func() {
		ui.Icon(c, icon).TextColor(t.TextMuted)
		ui.Text(c, label).Grow(1).SingleLine().Ellipsis("…")
		ui.Icon(c, iconUpDown).FontSize(11).TextColor(t.TextMuted)
	})
	ui.Popover(c, anchor, &s.pickerOpen, func() {
		ui.Column(c).Width(340).Gap(8).MinHeight(0).Children(func() {
			ui.SearchField(c.Key("deviceSearch"), &s.query).Label(locales.SearchDevices()).
				Placeholder(locales.SearchDevices()).AutoFocus()
			ui.Scroll(c.Key("deviceList")).MaxHeight(380).Children(func() {
				ui.Column(c).Gap(1).PaddingEnd(6).Children(func() { s.pickerItems(c, p) })
			})
		})
	})
}

func (s *Settings) pickerItems(c *ui.Context, p palette) {
	t := c.Theme()
	brands := devices.Search(s.query)
	var presets []config.Aspect
	q := strings.ToLower(strings.TrimSpace(s.query))
	for _, a := range config.Presets {
		if q == "" || strings.Contains(strings.ToLower(ratioLabel(a)), q) {
			presets = append(presets, a)
		}
	}
	if len(brands) == 0 && len(presets) == 0 {
		ui.Column(c).PaddingX(8).PaddingY(10).Gap(3).Children(func() {
			ui.Text(c, locales.NoDeviceMatch(strings.TrimSpace(s.query))).FontWeight(600)
			ui.Text(c, locales.NoDeviceMatchHint()).FontSize(12).TextColor(t.TextMuted)
		})
	}
	for _, b := range brands {
		groupHeader(c, brandLabel(b.Name, b.Zh))
		for _, m := range b.Models {
			if s.pickerRow(c, m.ID(), kindIcon(m.Kind), modelName(m), s.device == m.ID()) {
				s.chooseDevice(m)
			}
		}
	}
	groupHeader(c, locales.GroupOther())
	for _, a := range presets {
		if s.pickerRow(c, "ratio "+a.String(), iconDisplay, ratioLabel(a), s.device == "" && s.ratioKey == a.String()) {
			s.choosePreset(a)
		}
	}
	if s.pickerRow(c, "ratio custom", iconRatio, locales.RatioCustomItem(), s.device == "" && s.ratioKey == customRatio) {
		s.chooseCustom()
	}
	s.reveal = false
}

func groupHeader(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(11).FontWeight(600).TextColor(c.Theme().TextMuted).
		Padding(10, 8, 3, 8).SingleLine()
}

func (s *Settings) pickerRow(c *ui.Context, key string, icon *ui.SVG, title string, chosen bool) bool {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).FillWidth().Justify(ui.Start).Gap(8).PaddingX(8).PaddingY(5).Radius(6).Label(title)
	fg, muted := t.Text, t.TextMuted
	if chosen && s.reveal {
		b = b.ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollCenter})
	}
	if b.Hovered() {
		b = b.Background(t.Accent)
		fg, muted = t.AccentText, t.AccentText
	}
	b.Children(func() {
		ui.Icon(c, icon).TextColor(muted)
		ui.Text(c, title).TextColor(fg).Grow(1).SingleLine().Ellipsis("…")
		if chosen {
			ui.Icon(c, iconCheck).TextColor(fg)
		}
	})
	return b.Clicked()
}
