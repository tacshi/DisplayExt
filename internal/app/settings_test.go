package app

import (
	"slices"
	"testing"

	"displayext/internal/config"
	"displayext/internal/manager"
	"displayext/internal/uudetect"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
)

type fakeBackend struct {
	displays []config.Display
	wanted   map[string]bool
	failed   map[string]string
	reset    string
	login    bool
	language string
}

func newFake() *fakeBackend {
	return &fakeBackend{displays: config.Default().Displays, wanted: map[string]bool{}, failed: map[string]string{}}
}

func (f *fakeBackend) Displays() []config.Display { return f.displays }
func (f *fakeBackend) Status(id string) manager.Status {
	if err := f.failed[id]; err != "" && f.wanted[id] {
		return manager.Status{Err: err}
	}
	return manager.Status{Mounted: f.wanted[id], DisplayID: 7}
}
func (f *fakeBackend) Wanted(id string) bool         { return f.wanted[id] }
func (f *fakeBackend) SettingsReset() string         { return f.reset }
func (f *fakeBackend) UUState() uudetect.State       { return uudetect.Idle }
func (f *fakeBackend) Unavailable() string           { return "" }
func (f *fakeBackend) OpenAtLogin() bool             { return f.login }
func (f *fakeBackend) SetOpenAtLogin(on bool) error  { f.login = on; return nil }
func (f *fakeBackend) SetMounted(id string, on bool) { f.wanted[id] = on }
func (f *fakeBackend) Version() string               { return "0.1.0" }
func (f *fakeBackend) LogFile() string               { return "~/Library/Logs/DisplayExt/DisplayExt.log" }
func (f *fakeBackend) RevealLog()                    {}
func (f *fakeBackend) Language() string              { return f.language }
func (f *fakeBackend) SetLanguage(tag string)        { f.language = tag }
func (f *fakeBackend) Delete(id string) {
	f.displays = slices.DeleteFunc(f.displays, func(d config.Display) bool { return d.ID == id })
}
func (f *fakeBackend) Save(d config.Display) error {
	if err := d.Validate(); err != nil {
		return err
	}
	if i := slices.IndexFunc(f.displays, func(o config.Display) bool { return o.ID == d.ID }); i >= 0 {
		f.displays[i] = d
	} else {
		f.displays = append(f.displays, d)
	}
	return nil
}

func newTester(s *Settings) *ui.Tester { return ui.NewTester(s.View, 820, 1100) }

func clickRole(t *testing.T, tt *ui.Tester, role ui.Role, name string) {
	t.Helper()
	info, ok := tt.AccessByRole(role, name)
	if !ok {
		t.Fatalf("no %v named %q", role, name)
	}
	tt.ClickAt(info.Bounds.X+info.Bounds.W/2, info.Bounds.Y+info.Bounds.H/2)
}

func TestSettingsShowsDefault(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	for _, want := range []string{"iPad", "2048 × 1536", "HiDPI · 60 Hz", "Not mounted",
		"Sharp, Retina rendering at 4096 × 3072 pixels.", "Follows UU远程 · no session"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
}

func TestSettingsMountButton(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	clickRole(t, tt, ui.RoleButton, "Mount")
	if !f.wanted[f.displays[0].ID] {
		t.Fatal("Mount did not mount")
	}
	tt.Frame()
	for _, want := range []string{"Unmount", "Mounted · display 7"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q after mounting: %q", want, tt.Texts())
		}
	}
}

func TestSettingsEditAndSave(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)

	s.hz = 144
	tt.Frame()
	if err := tt.Click("Save"); err != nil {
		t.Fatal(err)
	}
	if got := f.displays[0].RefreshHz; got != 144 {
		t.Fatalf("saved refresh %d, want 144", got)
	}
	if s.dirty() {
		t.Error("still dirty after saving")
	}
}

func TestSettingsRefreshPreset(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	if err := tt.Click("Presets"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("120 Hz"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.hz != 120 || !s.dirty() {
		t.Errorf("preset: hz %v, dirty %v", s.hz, s.dirty())
	}
	if err := tt.Click("Revert"); err != nil {
		t.Fatal(err)
	}
	if s.hz != 60 || s.dirty() {
		t.Errorf("revert: hz %v, dirty %v", s.hz, s.dirty())
	}
}

func TestSettingsRatioChangeResetsSize(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	s.ratioKey = config.Presets[2].String()
	s.ratioChanged()
	d := s.compose()
	if d.AspectW != 16 || d.AspectH != 9 || d.Width != 2048 || d.Height != 1152 {
		t.Errorf("after switching to 16:9: %+v", d)
	}

	s.ratioKey = customRatio
	s.customW, s.customH = 2420, 1668
	s.ratioChanged()
	d = s.compose()
	if d.AspectW != 2420 || d.AspectH != 1668 || d.Validate() != nil {
		t.Errorf("custom ratio: %+v (%v)", d, d.Validate())
	}
}

func TestSettingsAddAndDelete(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	clickRole(t, tt, ui.RoleButton, "Add Display")
	if len(f.displays) != 2 || f.displays[1].Name != "Virtual Display" {
		t.Fatalf("after Add Display: %+v", f.displays)
	}
	if s.sel != f.displays[1].ID {
		t.Error("new display not selected")
	}
	tt.Frame()
	if err := tt.Click("Delete Display…"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(f.displays) != 2 {
		t.Fatal("deleted without asking")
	}
	if !tt.HasText("Delete “Virtual Display”?") {
		t.Fatalf("no confirmation: %q", tt.Texts())
	}
	clickRole(t, tt, ui.RoleButton, "Delete")
	tt.Frame()
	if len(f.displays) != 1 || s.sel != f.displays[0].ID {
		t.Errorf("after delete: %d displays, selected %q", len(f.displays), s.sel)
	}
}

func TestSettingsInvalidCannotSave(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	s.draft.Name = "  "
	tt.Frame()
	if !tt.HasText("Enter a name.") {
		t.Errorf("no validation error shown: %q", tt.Texts())
	}
	_ = tt.Click("Save")
	if f.displays[0].Name != "iPad" {
		t.Error("an invalid display was saved")
	}
}

func TestGeneralPage(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Open at login", "Not connected", "0.1.0", "Show in Finder", "Language", "System"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q on the General page: %q", want, tt.Texts())
		}
	}
	clickRole(t, tt, ui.RoleSwitch, "Open at login")
	tt.Frame()
	if !f.login {
		t.Error("switch did not turn on open at login")
	}
	if err := tt.Click("iPad"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if s.general || !tt.HasText("Looks like") {
		t.Error("choosing the display did not leave the General page")
	}
}

func TestNextName(t *testing.T) {
	ds := []config.Display{{Name: "Virtual Display"}, {Name: "Virtual Display 2"}}
	if got := nextName(ds); got != "Virtual Display 3" {
		t.Errorf("nextName = %q", got)
	}
}

func inLocale(t *testing.T, tag string) {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return tag })
	t.Cleanup(func() { i18n.SetLocaleFunc(func() string { return "" }) })
}

func TestSettingsInChinese(t *testing.T) {
	inLocale(t, "zh-Hans")
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	tt.SetLocale("zh-Hans")
	tt.Frame()
	for _, want := range []string{"显示器", "未挂载", "跟随 UU远程 · 无会话", "宽高比", "4:3 — iPad Pro 12.9/13 英寸", "以 4096 × 3072 像素进行清晰的 Retina 渲染。", "保存", "删除显示器…"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q in %q", want, tt.Texts())
		}
	}
	if s.ratioKey != "4:3" {
		t.Errorf("ratio key %q in Chinese", s.ratioKey)
	}
}

func TestLanguageChoice(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	if err := tt.Click("General"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("System"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("简体中文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if f.language != "zh-Hans" {
		t.Errorf("language %q after choosing 简体中文", f.language)
	}
}

func TestRatioSurvivesLanguageChange(t *testing.T) {
	f := newFake()
	f.displays[0].AspectW, f.displays[0].AspectH = 16, 9
	s := NewSettings(f)
	tt := newTester(s)
	tt.Frame()
	inLocale(t, "zh-Hans")
	tt.Frame()
	tt.Frame()
	if s.ratioKey != "16:9" || s.dirty() {
		t.Errorf("after switching language: key %q, dirty %v", s.ratioKey, s.dirty())
	}
}

func TestSettingsFailedDisplayCanBeTurnedOff(t *testing.T) {
	f := newFake()
	id := f.displays[0].ID
	f.wanted[id], f.failed[id] = true, "the WindowServer said no"
	s := NewSettings(f)
	tt := newTester(s)
	for _, want := range []string{"Failed to mount", "the WindowServer said no"} {
		if !tt.HasText(want) {
			t.Errorf("missing %q for a failed display: %q", want, tt.Texts())
		}
	}
	clickRole(t, tt, ui.RoleButton, "Unmount")
	if f.wanted[id] {
		t.Fatal("Unmount did not turn the failed display off")
	}
	tt.Frame()
	if !tt.HasText("Not mounted") || tt.HasText("the WindowServer said no") {
		t.Errorf("after turning it off: %q", tt.Texts())
	}
	clickRole(t, tt, ui.RoleButton, "Mount")
	if !f.wanted[id] {
		t.Error("Mount did not ask for the display again")
	}
}

func TestSettingsRatioOutOfRange(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	tt := newTester(s)
	s.ratioKey, s.ratio = customRatio, ratioKeyLabel(customRatio)
	s.customW, s.customH = 1, 10
	s.ratioChanged()
	tt.Frame()
	if !tt.HasText("The aspect ratio must be between 1:4 and 4:1.") {
		t.Errorf("no ratio problem shown: %q", tt.Texts())
	}
	_ = tt.Click("Save")
	if d := f.displays[0]; d.AspectW != 4 || d.AspectH != 3 {
		t.Errorf("an out-of-range ratio was saved: %+v", d)
	}
}

func TestSettingsPortraitRatio(t *testing.T) {
	f := newFake()
	s := NewSettings(f)
	s.ratioKey, s.ratio = customRatio, ratioKeyLabel(customRatio)
	for _, r := range []config.Aspect{{W: 9, H: 16}, {W: 1, H: 4}, {W: 3, H: 10}} {
		s.customW, s.customH = float64(r.W), float64(r.H)
		s.ratioChanged()
		for _, size := range s.sizeOptions() {
			s.size = size
			if p := problemText(s.compose()); p != "" {
				t.Errorf("%v offers %s, which is refused: %s", r, size, p)
			}
		}
	}
}

func TestSettingsResetNotice(t *testing.T) {
	f := newFake()
	f.reset = "/tmp/config-damaged-20261115-120000.json"
	tt := newTester(NewSettings(f))
	for _, want := range []string{"Your settings could not be read",
		"DisplayExt started over with its defaults and kept the old file as /tmp/config-damaged-20261115-120000.json."} {
		if !tt.HasText(want) {
			t.Errorf("missing %q: %q", want, tt.Texts())
		}
	}
}
