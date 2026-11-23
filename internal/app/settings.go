package app

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"displayext/internal/config"
	"displayext/internal/devices"
	"displayext/internal/manager"
	"displayext/internal/uudetect"
	"displayext/locales"

	"github.com/egoist/mygo/ui"
)

type Backend interface {
	Displays() []config.Display
	Status(id string) manager.Status
	Wanted(id string) bool
	UUStatus() uudetect.Status
	RustDeskStatus() uudetect.Status
	UUScreen() string
	UUDevice() string
	Devices() []string
	AccessibilityAllowed() bool
	UUWindowClosed() bool
	AllowAccessibility()
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
	triggers       = []config.Trigger{config.TriggerManual, config.TriggerUU, config.TriggerRustDesk}
	refreshPresets = []int{30, 60, 75, 90, 120, 144}
)

type Settings struct {
	b Backend

	sel     string
	pick    string
	general bool
	saved   config.Display
	draft   config.Display

	device           string
	ratioKey         string
	orientation      int
	customW, customH float64
	size             config.Size
	sizeChoice       string
	sizeShown        string
	hz               float64
	trigger          int
	autoName         bool
	connectFrom      string
	connectChoice    string
	connectShown     string

	pickerOpen bool
	query      string
	reveal     bool
	reselect   bool

	login         bool
	loginErr      string
	language      string
	saveErr       string
	confirmDelete bool
}

const (
	landscape = 0
	portrait  = 1
)

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
	if s.sizeChoice != s.sizeShown {
		for _, sz := range s.sizeChoices() {
			if s.sizeLabel(sz) == s.sizeChoice {
				s.size = sz
			}
		}
	}
	s.sizeChoice = s.sizeLabel(s.size)
	s.sizeShown = s.sizeChoice
	if s.connectChoice != s.connectShown {
		s.connectFrom = ""
		if s.connectChoice != locales.ConnectAny() {
			s.connectFrom = s.connectChoice
		}
	}
	s.connectChoice = connectLabel(s.connectFrom)
	s.connectShown = s.connectChoice
	if tag, ok := languageOf(s.language); ok && tag != s.b.Language() {
		s.b.SetLanguage(tag)
	}
	s.language = languageLabel(s.b.Language())
}

func (s *Settings) load(d config.Display) {
	a := d.AspectOf()
	s.orientation = landscape
	if a.W < a.H {
		s.orientation = portrait
	}
	s.device = ""
	if m, ok := devices.Lookup(d.Device); ok && m.Aspect(s.orientation == portrait) == a {
		s.device = d.Device
	} else {
		d.Device = ""
	}
	s.sel, s.pick, s.general, s.saved, s.draft = d.ID, d.ID, false, d, d
	s.saveErr = ""
	s.pickerOpen, s.query = false, ""
	s.ratioKey = customRatio
	for _, p := range config.Presets {
		if p == a || (config.Aspect{W: p.H, H: p.W}) == a {
			s.ratioKey = p.String()
		}
	}
	s.customW, s.customH = float64(d.AspectW), float64(d.AspectH)
	s.size = config.Size{W: d.Width, H: d.Height}
	s.sizeChoice = s.sizeLabel(s.size)
	s.sizeShown = s.sizeChoice
	s.hz = float64(d.RefreshHz)
	s.trigger = max(0, slices.Index(triggers, d.Trigger))
	s.connectFrom = d.ConnectFrom
	s.connectChoice = connectLabel(s.connectFrom)
	s.connectShown = s.connectChoice
	s.autoName = isAutoName(d.Name, d.Device)
}

func (s *Settings) model() (devices.Model, bool) { return devices.Lookup(s.device) }

func (s *Settings) aspect() config.Aspect {
	if m, ok := s.model(); ok {
		return m.Aspect(s.orientation == portrait)
	}
	if s.ratioKey == customRatio {
		return config.Aspect{W: int(math.Round(s.customW)), H: int(math.Round(s.customH))}
	}
	for _, a := range config.Presets {
		if a.String() == s.ratioKey {
			if s.orientation == portrait {
				return config.Aspect{W: a.H, H: a.W}
			}
			return a
		}
	}
	return config.Presets[0]
}

func (s *Settings) compose() config.Display {
	d := s.draft
	a := s.aspect()
	d.Device = s.device
	d.AspectW, d.AspectH = a.W, a.H
	d.Width, d.Height = s.size.W, s.size.H
	d.RefreshHz = int(math.Round(s.hz))
	d.Trigger = triggers[s.trigger]
	d.ConnectFrom = s.connectFrom
	d.Name = strings.TrimSpace(d.Name)
	return d
}

func (s *Settings) dirty() bool { return s.sel != "" && s.compose() != s.saved }

func (s *Settings) native() (config.Size, bool) {
	m, ok := s.model()
	if !ok {
		return config.Size{}, false
	}
	n := m.Sharpest(s.orientation == portrait)
	d := s.compose()
	d.Width, d.Height = n.W, n.H
	return n, d.Check() == config.NoProblem
}

func (s *Settings) sizeChoices() []config.Size {
	out := s.aspect().Sizes(s.draft.HiDPI)
	if n, ok := s.native(); ok && !slices.Contains(out, n) {
		out = append(out, n)
	}
	if s.size.W > 0 && !slices.Contains(out, s.size) {
		out = append(out, s.size)
	}
	slices.SortFunc(out, func(a, b config.Size) int { return max(a.W, a.H) - max(b.W, b.H) })
	return out
}

func (s *Settings) sizeLabel(sz config.Size) string {
	text := fmt.Sprintf("%d × %d", sz.W, sz.H)
	if n, ok := s.native(); ok && n == sz {
		m, _ := s.model()
		return locales.SizeNative(text, m.ShortName())
	}
	return text
}

func (s *Settings) sizeOptions() []string {
	sizes := s.sizeChoices()
	out := make([]string, len(sizes))
	for i, sz := range sizes {
		out[i] = s.sizeLabel(sz)
	}
	return out
}

func (s *Settings) defaultSize() config.Size {
	a := s.aspect()
	if m, ok := s.model(); ok && m.Kind == devices.Phone {
		if n, ok := s.native(); ok {
			return n
		}
		if sizes := a.Sizes(s.draft.HiDPI); len(sizes) > 0 {
			return sizes[0]
		}
	}
	return a.DefaultSize()
}

func (s *Settings) ratioChanged() {
	if a := s.aspect(); a.Valid() {
		s.size = s.defaultSize()
	}
}

func (s *Settings) chooseDevice(m devices.Model) {
	s.device, s.ratioKey = m.ID(), ""
	s.orientation = landscape
	if m.Kind == devices.Phone {
		s.orientation = portrait
	}
	s.pickerOpen, s.query = false, ""
	s.ratioChanged()
	if s.autoName {
		s.draft.Name = uniqueName(s.b.Displays(), m.ShortName(), s.sel)
	}
}

func (s *Settings) choosePreset(a config.Aspect) {
	s.device, s.ratioKey, s.orientation = "", a.String(), landscape
	s.pickerOpen, s.query = false, ""
	s.ratioChanged()
}

func (s *Settings) chooseCustom() {
	a := s.aspect()
	s.device, s.ratioKey = "", customRatio
	s.customW, s.customH = float64(a.W), float64(a.H)
	s.pickerOpen, s.query = false, ""
}

func (s *Settings) orientationChanged() {
	if s.ratioKey == customRatio && s.device == "" {
		return
	}
	s.ratioChanged()
}

func (s *Settings) addDisplay() {
	d := config.NewDisplay("", config.Presets[0])
	d.Device = config.DefaultDevice
	if m, ok := devices.Lookup(d.Device); ok {
		d.Name = uniqueName(s.b.Displays(), m.ShortName(), d.ID)
	}
	if err := s.b.Save(d); err != nil {
		s.saveErr = err.Error()
		return
	}
	s.load(d)
}

var genericName = regexp.MustCompile(`^(Virtual Display|iPad)( \d+)?$`)

func isAutoName(name, device string) bool {
	if name == "" || genericName.MatchString(name) {
		return true
	}
	if m, ok := devices.Lookup(device); ok {
		base := m.ShortName()
		return name == base || strings.HasPrefix(name, base+" ") && isNumber(strings.TrimPrefix(name, base+" "))
	}
	return false
}

func isNumber(s string) bool {
	_, err := strconv.Atoi(s)
	return err == nil
}

func uniqueName(ds []config.Display, base, self string) string {
	taken := func(n string) bool {
		return slices.ContainsFunc(ds, func(d config.Display) bool { return d.ID != self && d.Name == n })
	}
	for i := 1; ; i++ {
		n := base
		if i > 1 {
			n = fmt.Sprintf("%s %d", base, i)
		}
		if !taken(n) {
			return n
		}
	}
}

func (s *Settings) needsRemount() bool {
	d := s.compose()
	bw, bh := d.Box()
	sw, sh := s.saved.Box()
	return d.Name != s.saved.Name || d.Serial != s.saved.Serial || bw != sw || bh != sh
}

func (s *Settings) save() {
	d := s.compose()
	reshaped := d.AspectOf() != s.saved.AspectOf() && !s.needsRemount() && s.showingThis()
	if d.Check() != config.NoProblem {
		s.saveErr = problemText(d)
		return
	}
	if err := s.b.Save(d); err != nil {
		s.saveErr = err.Error()
		return
	}
	s.load(d)
	s.reselect = reshaped
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
			if !s.general && s.sel != "" {
				s.footer(c, p)
			}
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

func (s *Settings) showingThis() bool {
	st := s.b.Status(s.sel)
	uu := s.b.UUStatus()
	return st.Mounted && uu.Showing != 0 && uu.Showing == st.DisplayID
}

func (s *Settings) needsSwitch() bool {
	uu := s.b.UUStatus()
	return s.b.Status(s.sel).Mounted && uu.State == uudetect.Connected && uu.Showing != 0 && !s.showingThis()
}

func (s *Settings) summary() string {
	switch s.saved.Trigger {
	case config.TriggerUU:
	case config.TriggerRustDesk:
		switch s.b.RustDeskStatus().State {
		case uudetect.Connected:
			return locales.SummaryRustDeskConnected()
		case uudetect.Idle:
			return locales.SummaryRustDeskIdle()
		}
		return locales.SummaryRustDeskNotRunning()
	default:
		return locales.SummaryManual()
	}
	uu := s.b.UUStatus()
	switch uu.State {
	case uudetect.Connected:
		switch device := s.b.UUDevice(); {
		case s.saved.ConnectFrom != "" && device != "" && device != s.saved.ConnectFrom:
			return locales.SummaryUUOtherDevice(device)
		case uu.Showing == 0:
			return locales.SummaryUUConnected()
		case s.showingThis():
			return locales.SummaryUUShowingThis()
		}
		return locales.SummaryUUShowingOther(s.b.UUScreen())
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

	s.hero(c, p, d)

	section(c, p, locales.SectionDisplay(), func() {
		row(c, p, true, locales.FieldName(), "", func() {
			if ui.TextInput(c.Key("name"), &s.draft.Name).Width(240).Label(locales.FieldName()).Changed() {
				s.autoName = false
			}
		})
		m, isDevice := s.model()
		row(c, p, false, locales.FieldDevice(), deviceHelp(m, isDevice), func() {
			s.devicePicker(c, p)
		})
		if isDevice || s.ratioKey != customRatio {
			row(c, p, false, locales.FieldOrientation(), "", func() {
				if ui.Segmented(c.Key("orientation"), &s.orientation, locales.OrientationLandscape(), locales.OrientationPortrait()).
					Label(locales.FieldOrientation()).Changed() {
					s.orientationChanged()
				}
			})
		}
		if s.ratioKey == customRatio && !isDevice {
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
			ui.Select(c.Key("size"), &s.sizeChoice, s.sizeOptions()).Width(240).Label(locales.FieldLooksLike())
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
			ui.Segmented(c.Key("trigger"), &s.trigger, locales.TriggerManual(), locales.TriggerUU(), locales.TriggerRustDesk()).Label(locales.LabelWhenToMount())
		})
		if triggers[s.trigger] == config.TriggerUU {
			s.connectRow(c, p)
		}
		row(c, p, false, locales.FieldScreensOff(), locales.HelpScreensOff(), func() {
			ui.Switch(c.Key("screensOff"), &s.draft.ScreensOff).Label(locales.FieldScreensOff())
		})
	})

}

func (s *Settings) footer(c *ui.Context, p palette) {
	t := c.Theme()
	problem := problemText(s.compose())
	dirty := s.dirty()
	ui.Column(c).Padding(12, 28, 14, 28).BorderWidth(1, 0, 0, 0).BorderColor(p.hairline).Background(p.page).Children(func() {
		ui.Column(c).MaxWidth(640).Gap(6).Children(func() {
			if problem != "" {
				ui.Text(c, problem).TextColor(t.Danger)
			} else if s.saveErr != "" {
				ui.Text(c, s.saveErr).TextColor(t.Danger)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Button(c, locales.ButtonDelete()).TextColor(t.Danger).OnClick(func() { s.confirmDelete = true })
				ui.Spacer(c)
				switch {
				case dirty && s.b.Status(s.sel).Mounted && s.needsRemount():
					ui.Text(c, locales.NoteRemount()).FontSize(11.5).TextColor(t.TextMuted)
				case dirty:
					ui.Text(c, locales.NoteUnsaved()).FontSize(11.5).TextColor(t.TextMuted)
				}
				ui.Button(c, locales.ButtonRevert()).Disabled(!dirty).OnClick(func() { s.load(s.saved) })
				ui.PrimaryButton(c, locales.ButtonSave()).Disabled(!dirty || problem != "").OnClick(s.save)
			})
		})
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
				name := d.Name
				if name == "" {
					name = s.saved.Name
				}
				ui.Text(c, name).FontSize(19).Bold().SingleLine()
				if m, ok := devices.Lookup(d.Device); ok && modelTitle(m) != name {
					ui.Text(c, modelTitle(m)).FontSize(12).TextColor(t.TextMuted).SingleLine().MarginY(-3)
				}
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
				if s.reselect && !s.showingThis() {
					s.reselect = false
				}
				if s.reselect {
					ui.Text(c, locales.HintReselect()).FontSize(12).FontWeight(600).TextColor(t.Accent)
				}
				if s.needsSwitch() {
					ui.Text(c, locales.HintChooseInUU(s.saved.Name)).FontSize(12).FontWeight(600).TextColor(t.Accent)
				}
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
		st := s.b.UUStatus()
		row(c, p, true, locales.FieldSession(), locales.HelpSession(), func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				col := t.TextMuted
				if st.State == uudetect.Connected {
					col = t.Success
				}
				dot(c, col)
				ui.Text(c, uuLabel(st, s.b.UUDevice(), s.b.UUScreen())).TextColor(t.TextMuted)
			})
		})
	})
	section(c, p, locales.SectionRustDesk(), func() {
		st := s.b.RustDeskStatus()
		row(c, p, true, locales.FieldSession(), locales.HelpSessionRustDesk(), func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				col := t.TextMuted
				if st.State == uudetect.Connected {
					col = t.Success
				}
				dot(c, col)
				ui.Text(c, sessionLabel(st.State)).TextColor(t.TextMuted)
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
	maxH := float32(106)
	if d.AspectW < d.AspectH {
		maxH = 132
	}
	w, h := fit(d.AspectW, d.AspectH, 156, maxH)
	narrow := w < 120
	ui.Column(c).Width(156).AlignItems(ui.Center).Children(func() {
		screen := ui.Column(c).Size(w, h).Radius(10).Gap(2).Center()
		if mounted {
			screen = screen.Gradient(p.screenTop, p.screenBottom, 160).Border(4, p.frame)
		} else {
			screen = screen.Background(p.ghost).Border(2.5, p.frame).BorderStyle(ui.BorderDashed)
		}
		screen.Children(func() {
			size := func(text string) { ui.Text(c, text).FontSize(13).FontWeight(600).SingleLine() }
			detail := func(text string) { ui.Text(c, text).FontSize(11).TextColor(t.TextMuted).SingleLine() }
			hz := fmt.Sprintf("%d Hz", d.RefreshHz)
			switch {
			case narrow:
				size(strconv.Itoa(d.Width))
				size("× " + strconv.Itoa(d.Height))
				if d.HiDPI {
					detail("HiDPI")
				}
				detail(hz)
			case d.HiDPI:
				size(fmt.Sprintf("%d × %d", d.Width, d.Height))
				detail("HiDPI · " + hz)
			default:
				size(fmt.Sprintf("%d × %d", d.Width, d.Height))
				detail(hz)
			}
		})
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

func connectLabel(device string) string {
	if device == "" {
		return locales.ConnectAny()
	}
	return device
}

func (s *Settings) connectOptions() []string {
	out := []string{locales.ConnectAny()}
	out = append(out, s.b.Devices()...)
	if s.connectFrom != "" && !slices.Contains(out, s.connectFrom) {
		out = append(out, s.connectFrom)
	}
	return out
}

func (s *Settings) connectRow(c *ui.Context, p palette) {
	if !s.b.AccessibilityAllowed() {
		row(c, p, false, locales.FieldConnectFrom(), locales.HelpConnectAllow(), func() {
			ui.Button(c.Key("allowAX"), locales.ButtonAllow()).OnClick(s.b.AllowAccessibility)
		})
		return
	}
	help := locales.HelpConnectAny()
	switch {
	case s.b.UUWindowClosed():
		help = locales.HelpConnectWindowClosed()
	case s.connectFrom != "":
		help = locales.HelpConnectDevice(s.connectFrom)
	case len(s.b.Devices()) == 0:
		help = locales.HelpConnectNone()
	}
	row(c, p, false, locales.FieldConnectFrom(), help, func() {
		ui.Select(c.Key("connectFrom"), &s.connectChoice, s.connectOptions()).Width(240).Label(locales.FieldConnectFrom())
	})
}
