package app

import (
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"displayext/internal/config"
	"displayext/internal/manager"
	"displayext/internal/uudetect"
	"displayext/locales"

	"github.com/egoist/mygo/ui"
)

type Backend interface {
	Displays() []config.Display
	Status(id string) manager.Status
	Wanted(id string) bool
	UUState() uudetect.State
	Unavailable() string
	SettingsReset() string

	Save(d config.Display) error
	Delete(id string)
	SetMounted(id string, mounted bool)

	OpenAtLogin() bool
	SetOpenAtLogin(on bool) error

	Language() string
	SetLanguage(tag string)

	Version() string
	LogFile() string
	RevealLog()
}

const generalPage = "general"

var (
	triggers       = []config.Trigger{config.TriggerManual, config.TriggerUU}
	refreshPresets = []int{30, 60, 75, 90, 120, 144}
)

type Settings struct {
	b Backend

	sel     string
	pick    string
	general bool
	saved   config.Display
	draft   config.Display

	ratioKey         string
	ratio            string
	customW, customH float64
	size             string
	hz               float64
	trigger          int

	login         bool
	loginErr      string
	language      string
	saveErr       string
	confirmDelete bool
}

func NewSettings(b Backend) *Settings {
	s := &Settings{b: b, login: b.OpenAtLogin()}
	if ds := b.Displays(); len(ds) > 0 {
		s.load(ds[0])
	} else {
		s.showGeneral()
	}
	return s
}

func (s *Settings) Refresh() {
	s.login = s.b.OpenAtLogin()
	if s.general {
		return
	}
	ds := s.b.Displays()
	i := slices.IndexFunc(ds, func(d config.Display) bool { return d.ID == s.sel })
	switch {
	case i < 0 && len(ds) > 0:
		s.load(ds[0])
	case i < 0:
		s.sel, s.pick = "", ""
	case ds[i] != s.saved && !s.dirty():
		s.load(ds[i])
	}
}

func (s *Settings) showGeneral() {
	s.general, s.pick = true, generalPage
}

func (s *Settings) current() string {
	if s.general {
		return generalPage
	}
	return s.sel
}

func (s *Settings) syncPick() {
	if s.pick == s.current() {
		return
	}
	if s.pick == generalPage {
		s.showGeneral()
		return
	}
	for _, d := range s.b.Displays() {
		if d.ID == s.pick {
			s.load(d)
			return
		}
	}
	s.pick = s.current()
}

func (s *Settings) syncChoices() {
	if key, ok := ratioKeyOf(s.ratio); ok {
		s.ratioKey = key
	}
	s.ratio = ratioKeyLabel(s.ratioKey)
	if tag, ok := languageOf(s.language); ok && tag != s.b.Language() {
		s.b.SetLanguage(tag)
	}
	s.language = languageLabel(s.b.Language())
}

func (s *Settings) load(d config.Display) {
	s.sel, s.pick, s.general, s.saved, s.draft = d.ID, d.ID, false, d, d
	s.saveErr = ""
	s.ratioKey = customRatio
	for _, a := range config.Presets {
		if a.W == d.AspectW && a.H == d.AspectH {
			s.ratioKey = a.String()
		}
	}
	s.ratio = ratioKeyLabel(s.ratioKey)
	s.customW, s.customH = float64(d.AspectW), float64(d.AspectH)
	s.size = config.Size{W: d.Width, H: d.Height}.String()
	s.hz = float64(d.RefreshHz)
	s.trigger = max(0, slices.Index(triggers, d.Trigger))
}

func (s *Settings) aspect() config.Aspect {
	for _, a := range config.Presets {
		if a.String() == s.ratioKey {
			return a
		}
	}
	return config.Aspect{W: int(math.Round(s.customW)), H: int(math.Round(s.customH))}
}

func parseSize(v string) (config.Size, bool) {
	w, h, ok := strings.Cut(v, "×")
	if !ok {
		return config.Size{}, false
	}
	wi, err1 := strconv.Atoi(w)
	hi, err2 := strconv.Atoi(h)
	return config.Size{W: wi, H: hi}, err1 == nil && err2 == nil
}

func (s *Settings) compose() config.Display {
	d := s.draft
	a := s.aspect()
	d.AspectW, d.AspectH = a.W, a.H
	if sz, ok := parseSize(s.size); ok {
		d.Width, d.Height = sz.W, sz.H
	}
	d.RefreshHz = int(math.Round(s.hz))
	d.Trigger = triggers[s.trigger]
	d.Name = strings.TrimSpace(d.Name)
	return d
}

func (s *Settings) dirty() bool { return s.sel != "" && s.compose() != s.saved }

func (s *Settings) sizeOptions() []string {
	var out []string
	for _, sz := range s.aspect().Sizes() {
		out = append(out, sz.String())
	}
	if s.size != "" && !slices.Contains(out, s.size) {
		out = append([]string{s.size}, out...)
	}
	return out
}

func (s *Settings) ratioChanged() {
	if a := s.aspect(); a.Valid() {
		s.size = a.DefaultSize().String()
	}
}

func (s *Settings) addDisplay() {
	d := config.NewDisplay(nextName(s.b.Displays()), config.Presets[0])
	if err := s.b.Save(d); err != nil {
		s.saveErr = err.Error()
		return
	}
	s.load(d)
}

func nextName(ds []config.Display) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(ds, func(d config.Display) bool { return d.Name == n })
	}
	for i := 1; ; i++ {
		n := "Virtual Display"
		if i > 1 {
			n = fmt.Sprintf("Virtual Display %d", i)
		}
		if !taken(n) {
			return n
		}
	}
}

func (s *Settings) save() {
	d := s.compose()
	if d.Check() != config.NoProblem {
		s.saveErr = problemText(d)
		return
	}
	if err := s.b.Save(d); err != nil {
		s.saveErr = err.Error()
		return
	}
	s.load(d)
}

func (s *Settings) remove() {
	s.b.Delete(s.sel)
	s.sel, s.pick = "", ""
	s.Refresh()
}

func (s *Settings) View(c *ui.Context) {
	s.syncPick()
	s.syncChoices()
	p := colors(c.Theme())
	bar := c.TitleBar()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(p.page).Children(func() {
		s.sidebar(c, p, bar)
		ui.Column(c).Grow(1).Children(func() {
			ui.Row(c).Height(max(bar.Height, 52)).PaddingX(28).AlignItems(ui.Center).DragWindow().Children(func() {
				ui.Text(c, s.title()).FontSize(15).Bold().SingleLine()
			})
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Padding(4, 28, 28, 28).Gap(22).MaxWidth(640).Children(func() {
					if msg := s.b.Unavailable(); msg != "" {
						notice(c, p, locales.UnavailableTitle(), msg)
					}
					if kept := s.b.SettingsReset(); kept != "" {
						notice(c, p, locales.ResetTitle(), locales.ResetBody(kept))
					}
					switch {
					case s.general:
						s.generalPage(c, p)
					case s.sel == "":
						s.emptyPage(c, p)
					default:
						s.displayPage(c, p)
					}
				})
			})
		})
	})
	if ui.AlertDialog(c, &s.confirmDelete, locales.DeleteTitle(s.saved.Name), locales.DeleteMessage(),
		locales.ButtonCancel(), locales.ButtonConfirmDelete()) == 1 {
		s.remove()
	}
}

func (s *Settings) title() string {
	if s.general {
		return locales.SidebarGeneral()
	}
	return locales.SidebarDisplays()
}

func (s *Settings) summary() string {
	if s.saved.Trigger != config.TriggerUU {
		return locales.SummaryManual()
	}
	switch s.b.UUState() {
	case uudetect.Connected:
		return locales.SummaryUUConnected()
	case uudetect.Idle:
		return locales.SummaryUUIdle()
	}
	return locales.SummaryUUNotRunning()
}

func (s *Settings) sidebar(c *ui.Context, p palette, bar ui.TitleBar) {
	t := c.Theme()
	ui.Column(c).Width(220).Background(p.sidebar).BorderWidth(0, 1, 0, 0).BorderColor(p.hairline).Children(func() {
		ui.Box(c).FillWidth().Height(max(bar.Bottom+10, 12)).DragWindow()
		ui.Sidebar(c, &s.pick, func() {
			ui.SidebarSection(c, locales.SidebarDisplays(), nil, func() {
				for _, d := range s.b.Displays() {
					ui.SidebarItem(c.Key(d.ID), d.ID, iconDisplay, d.Name).Children(func() {
						switch st := s.b.Status(d.ID); {
						case st.Mounted:
							dot(c, t.Success).Tooltip(locales.TooltipMounted())
						case st.Err != "":
							dot(c, t.Danger).Tooltip(st.Err)
						}
					})
				}
			})
			ui.SidebarSection(c, locales.SidebarApp(), nil, func() {
				ui.SidebarItem(c, generalPage, iconGear, locales.SidebarGeneral())
			})
		}).Grow(1).PaddingX(10).Background(p.sidebar)
		ui.Row(c).Padding(10, 12, 14, 12).Children(func() {
			ui.Button(c, "").Label(locales.AddDisplay()).FillWidth().OnClick(s.addDisplay).Children(func() {
				ui.Icon(c, iconPlus)
				ui.Text(c, locales.AddDisplay()).SingleLine()
			})
		})
	})
}

func (s *Settings) emptyPage(c *ui.Context, p palette) {
	t := c.Theme()
	card(c, p).Padding(36).Gap(12).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, iconDisplay).FontSize(40).TextColor(t.TextMuted)
		ui.Text(c, locales.EmptyTitle()).FontSize(15).Bold()
		ui.Text(c, locales.EmptyBody()).TextColor(t.TextMuted).TextAlign(ui.Center)
		ui.PrimaryButton(c, locales.AddDisplay()).OnClick(s.addDisplay)
	})
}

func (s *Settings) displayPage(c *ui.Context, p palette) {
	t := c.Theme()
	d := s.compose()
	problem := problemText(d)

	s.hero(c, p, d)

	section(c, p, locales.SectionDisplay(), func() {
		row(c, p, true, locales.FieldName(), "", func() {
			ui.TextInput(c.Key("name"), &s.draft.Name).Width(240).Label(locales.FieldName())
		})
		row(c, p, false, locales.FieldAspect(), "", func() {
			if ui.Select(c.Key("ratio"), &s.ratio, ratioOptions()).Width(240).Label(locales.FieldAspect()).Changed() {
				s.ratioChanged()
			}
		})
		if s.ratioKey == customRatio {
			row(c, p, false, locales.FieldCustomRatio(), locales.HelpCustomRatio(), func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					w := ui.NumberInput(c.Key("ratioW"), &s.customW, 1, 10000, 1).Width(112).Label(locales.LabelRatioWidth()).Changed()
					ui.Text(c, ":").TextColor(t.TextMuted)
					h := ui.NumberInput(c.Key("ratioH"), &s.customH, 1, 10000, 1).Width(112).Label(locales.LabelRatioHeight()).Changed()
					if w || h {
						s.ratioChanged()
					}
				})
			})
		}
		row(c, p, false, locales.FieldLooksLike(), locales.HelpLooksLike(), func() {
			ui.Select(c.Key("size"), &s.size, s.sizeOptions()).Width(240).Label(locales.FieldLooksLike())
		})
		pw, ph := d.PixelSize()
		row(c, p, false, locales.FieldHiDPI(), locales.HelpHiDPI(pw, ph), func() {
			ui.Switch(c.Key("hidpi"), &s.draft.HiDPI).Label(locales.FieldHiDPI())
		})
		row(c, p, false, locales.FieldRefresh(), locales.HelpRefresh(), func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.NumberInput(c.Key("hz"), &s.hz, config.MinRefresh, config.MaxRefresh, 1).Width(112).Label(locales.LabelRefreshHz())
				ui.Text(c, "Hz").TextColor(t.TextMuted)
				ui.MenuButton(c.Key("presets"), locales.Presets(), func(m *ui.Menu) {
					for _, hz := range refreshPresets {
						if m.Item(fmt.Sprintf("%d Hz", hz)).Checked(int(math.Round(s.hz)) == hz).Chosen() {
							s.hz = float64(hz)
						}
					}
				})
			})
		})
	})

	section(c, p, locales.SectionMounting(), func() {
		row(c, p, true, locales.FieldMount(), triggerHelp(triggers[s.trigger]), func() {
			ui.Segmented(c.Key("trigger"), &s.trigger, locales.TriggerManual(), locales.TriggerUU()).Label(locales.LabelWhenToMount())
		})
		row(c, p, false, locales.FieldScreensOff(), locales.HelpScreensOff(), func() {
			ui.Switch(c.Key("screensOff"), &s.draft.ScreensOff).Label(locales.FieldScreensOff())
		})
	})

	if problem != "" {
		ui.Text(c, problem).TextColor(t.Danger)
	} else if s.saveErr != "" {
		ui.Text(c, s.saveErr).TextColor(t.Danger)
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Button(c, locales.ButtonDelete()).TextColor(t.Danger).OnClick(func() { s.confirmDelete = true })
		ui.Spacer(c)
		if s.dirty() && s.b.Status(s.sel).Mounted {
			ui.Text(c, locales.NoteRemount()).FontSize(11.5).TextColor(t.TextMuted)
		}
		ui.Button(c, locales.ButtonRevert()).Disabled(!s.dirty()).OnClick(func() { s.load(s.saved) })
		ui.PrimaryButton(c, locales.ButtonSave()).Disabled(!s.dirty() || problem != "").OnClick(s.save)
	})
}

func (s *Settings) hero(c *ui.Context, p palette, d config.Display) {
	t := c.Theme()
	st := s.b.Status(s.sel)
	mounted := st.Mounted
	wanted := s.b.Wanted(s.sel)
	card(c, p).Padding(18).Gap(12).Children(func() {
		ui.Row(c).Gap(20).AlignItems(ui.Center).Children(func() {
			preview(c, p, d, mounted)
			ui.Column(c).Grow(1).Gap(7).Children(func() {
				ui.Text(c, s.saved.Name).FontSize(19).Bold().SingleLine()
				switch {
				case st.Busy:
					pill(c, locales.Working(), t.TextMuted)
				case mounted:
					pill(c, locales.StatusMounted(int(st.DisplayID)), t.Success)
				case st.Err != "":
					pill(c, locales.StatusFailed(), t.Danger)
				default:
					pill(c, locales.StatusNotMounted(), t.TextMuted)
				}
				ui.Text(c, s.summary()).FontSize(12).TextColor(t.TextMuted)
			})
			label := locales.ButtonMount()
			button := ui.PrimaryButton
			if wanted {
				label, button = locales.ButtonUnmount(), ui.Button
			}
			button(c.Key("mount"), label).Disabled(st.Busy || s.b.Unavailable() != "" || (!wanted && s.saved.Check() != config.NoProblem)).
				OnClick(func() { s.b.SetMounted(s.sel, !wanted) })
		})
		if st.Err != "" {
			ui.Text(c, st.Err).FontSize(12).TextColor(t.Danger)
		}
		if st.ModeErr != "" {
			ui.Text(c, locales.ModeNotApplied(st.ModeErr)).FontSize(12).TextColor(t.Warning)
		}
		if st.ScreensErr != "" {
			ui.Text(c, locales.ScreensNotOff(st.ScreensErr)).FontSize(12).TextColor(t.Warning)
		}
	})
}

func (s *Settings) generalPage(c *ui.Context, p palette) {
	t := c.Theme()
	section(c, p, locales.SectionStartup(), func() {
		help := locales.HelpOpenAtLogin()
		if s.loginErr != "" {
			help = s.loginErr
		}
		row(c, p, true, locales.FieldOpenAtLogin(), help, func() {
			if ui.Switch(c.Key("login"), &s.login).Label(locales.FieldOpenAtLogin()).Changed() {
				s.loginErr = ""
				if err := s.b.SetOpenAtLogin(s.login); err != nil {
					s.loginErr = err.Error()
				}
				s.login = s.b.OpenAtLogin()
			}
		})
	})
	section(c, p, locales.SectionLanguage(), func() {
		row(c, p, true, locales.FieldLanguage(), locales.HelpLanguage(), func() {
			ui.Select(c.Key("language"), &s.language, languageOptions()).Width(200).Label(locales.FieldLanguage())
		})
	})
	section(c, p, locales.SectionUU(), func() {
		st := s.b.UUState()
		row(c, p, true, locales.FieldSession(), locales.HelpSession(), func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				col := t.TextMuted
				if st == uudetect.Connected {
					col = t.Success
				}
				dot(c, col)
				ui.Text(c, uuLabel(st)).TextColor(t.TextMuted)
			})
		})
	})
	section(c, p, locales.SectionAbout(), func() {
		row(c, p, true, locales.FieldVersion(), "", func() {
			ui.Text(c, s.b.Version()).TextColor(t.TextMuted)
		})
		row(c, p, false, locales.FieldLogFile(), s.b.LogFile(), func() {
			ui.Button(c.Key("reveal"), locales.ButtonShowInFinder()).OnClick(s.b.RevealLog)
		})
	})
}

type palette struct {
	page, sidebar, card, hairline  ui.Color
	screenTop, screenBottom, frame ui.Color
	ghost                          ui.Color
}

func colors(t *ui.Theme) palette {
	if t.Dark {
		return palette{
			page: ui.Hex("#1D1D20"), sidebar: ui.Hex("#242427"), card: ui.Hex("#2B2B2F"),
			hairline:  ui.RGBA(255, 255, 255, 0.08),
			screenTop: ui.Hex("#4A5262"), screenBottom: ui.Hex("#2E3440"), frame: ui.Hex("#D9DDE3"),
			ghost: ui.RGBA(255, 255, 255, 0.04),
		}
	}
	return palette{
		page: ui.Hex("#F5F5F7"), sidebar: ui.Hex("#ECECEF"), card: ui.Hex("#FFFFFF"),
		hairline:  ui.RGBA(0, 0, 0, 0.08),
		screenTop: ui.Hex("#EEF1F5"), screenBottom: ui.Hex("#C5CDD8"), frame: ui.Hex("#2A2D33"),
		ghost: ui.RGBA(42, 45, 51, 0.04),
	}
}

func card(c *ui.Context, p palette) ui.Element {
	return ui.Column(c).Background(p.card).Radius(12).Border(1, p.hairline).
		Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.04))
}

func section(c *ui.Context, p palette, title string, rows func()) {
	ui.Column(c).Gap(8).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).MarginStart(4)
		card(c, p).Children(rows)
	})
}

func row(c *ui.Context, p palette, first bool, label, help string, control func()) {
	t := c.Theme()
	r := ui.Row(c).PaddingX(16).PaddingY(11).Gap(18).AlignItems(ui.Center)
	if !first {
		r = r.BorderWidth(1, 0, 0, 0).BorderColor(p.hairline)
	}
	r.Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, label)
			if help != "" {
				ui.Text(c, help).FontSize(11.5).TextColor(t.TextMuted)
			}
		})
		control()
	})
}

func preview(c *ui.Context, p palette, d config.Display, mounted bool) {
	t := c.Theme()
	w, h := fit(d.AspectW, d.AspectH, 156, 106)
	screen := ui.Column(c).Size(w, h).Radius(10).Gap(2).Center()
	if mounted {
		screen = screen.Gradient(p.screenTop, p.screenBottom, 160).Border(4, p.frame)
	} else {
		screen = screen.Background(p.ghost).Border(2.5, p.frame).BorderStyle(ui.BorderDashed)
	}
	screen.Children(func() {
		ui.Text(c, fmt.Sprintf("%d × %d", d.Width, d.Height)).FontSize(13).FontWeight(600).SingleLine()
		detail := fmt.Sprintf("%d Hz", d.RefreshHz)
		if d.HiDPI {
			detail = "HiDPI · " + detail
		}
		ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted).SingleLine()
	})
}

func fit(aw, ah int, maxW, maxH float32) (float32, float32) {
	if aw <= 0 || ah <= 0 {
		return maxW, maxH
	}
	r := float32(aw) / float32(ah)
	if maxW/r <= maxH {
		return maxW, maxW / r
	}
	return maxH * r, maxH
}

func pill(c *ui.Context, text string, col ui.Color) {
	bg := col
	bg.A = 34
	ui.Row(c).AlignSelf(ui.Start).PaddingX(9).PaddingY(3).Radius(999).Background(bg).Children(func() {
		ui.Text(c, text).FontSize(11.5).FontWeight(600).TextColor(col).SingleLine()
	})
}

func dot(c *ui.Context, col ui.Color) ui.Element {
	return ui.Box(c).Size(8, 8).Radius(4).Background(col)
}

func notice(c *ui.Context, p palette, title, detail string) {
	t := c.Theme()
	card(c, p).Padding(14).Gap(4).BorderColor(t.Danger).Children(func() {
		ui.Text(c, title).FontWeight(600).TextColor(t.Danger)
		ui.Text(c, detail).FontSize(12).TextColor(t.TextMuted)
	})
}
