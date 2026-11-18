package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSizesFourThree(t *testing.T) {
	got := Aspect{W: 4, H: 3}.Sizes(true)
	want := []Size{
		{1024, 768}, {1280, 960}, {1440, 1080}, {1600, 1200}, {1920, 1440},
		{2048, 1536}, {2304, 1728}, {2560, 1920}, {2880, 2160}, {3200, 2400},
	}
	if !slices.Equal(got, want) {
		t.Errorf("4:3 sizes\n got %v\nwant %v", got, want)
	}
}

func TestDefaultSizes(t *testing.T) {
	cases := map[Aspect]Size{
		{W: 4, H: 3}:   {2048, 1536},
		{W: 16, H: 10}: {2048, 1280},
		{W: 16, H: 9}:  {2048, 1152},
		{W: 3, H: 4}:   {1536, 2048},
	}
	for a, want := range cases {
		if got := a.DefaultSize(); got != want {
			t.Errorf("%v default size = %v, want %v", a, got, want)
		}
	}
}

func TestCustomRatioSizes(t *testing.T) {
	sizes := Aspect{W: 605, H: 417}.Sizes(true)
	if len(sizes) == 0 {
		t.Fatal("no sizes for a custom ratio")
	}
	for _, s := range sizes {
		if s.W%8 != 0 {
			t.Errorf("width %d is not a multiple of 8", s.W)
		}
	}
}

func TestInvalidAspect(t *testing.T) {
	for _, a := range []Aspect{{0, 3}, {4, 0}, {-4, 3}, {100, 1}} {
		if a.Valid() || a.Sizes(true) != nil {
			t.Errorf("%v should be invalid with no sizes", a)
		}
	}
}

func TestPixelSizeAndModes(t *testing.T) {
	d := Default().Displays[0]
	if w, h := d.PixelSize(); w != 4096 || h != 3072 {
		t.Fatalf("HiDPI iPad pixel size = %d×%d, want 4096×3072", w, h)
	}
	modes := d.Modes()
	if len(modes) == 0 || modes[0] != (Size{2048, 1536}) {
		t.Errorf("modes %v do not start with the display's own size in points", modes)
	}
	for _, want := range []Size{{1024, 768}, {3200, 2400}} {
		if !slices.Contains(modes, want) {
			t.Errorf("modes %v lack %v", modes, want)
		}
	}
	if w, h := d.Box(); w != PixelBox || h != PixelBox {
		t.Errorf("box %d×%d", w, h)
	}
	for _, m := range modes {
		if m.W*2 > PixelBox || m.H*2 > PixelBox {
			t.Errorf("HiDPI mode %v does not fit the box", m)
		}
	}
	d.Width, d.Height = 4000, 3000
	if w, _ := d.Box(); w != 8000 {
		t.Errorf("a size larger than the box gets a %d box", w)
	}
	d.Width, d.Height = 2048, 1536

	d.HiDPI = false
	if w, h := d.PixelSize(); w != 2048 || h != 1536 {
		t.Errorf("1x pixel size = %d×%d, want 2048×1536", w, h)
	}
}

func TestDefaultIsValid(t *testing.T) {
	c := Default()
	if len(c.Displays) != 1 {
		t.Fatalf("default has %d displays", len(c.Displays))
	}
	d := c.Displays[0]
	if err := d.Validate(); err != nil {
		t.Fatal(err)
	}
	if d.Trigger != TriggerUU || d.Name != "iPad" || d.RefreshHz != 60 || !d.HiDPI {
		t.Errorf("unexpected default display %+v", d)
	}
	if c.UU != DefaultUURule() {
		t.Errorf("default UU rule %+v", c.UU)
	}
	if d.Label() != "iPad · 2048×1536 · 4:3" {
		t.Errorf("label %q", d.Label())
	}
}

func TestValidate(t *testing.T) {
	base := Default().Displays[0]
	bad := map[string]func(*Display){
		"no name":         func(d *Display) { d.Name = "" },
		"zero ratio":      func(d *Display) { d.AspectH = 0 },
		"ratio 1:10":      func(d *Display) { d.AspectW, d.AspectH = 1, 10 },
		"ratio 10:1":      func(d *Display) { d.AspectW, d.AspectH = 10, 1 },
		"tiny":            func(d *Display) { d.Width = 100 },
		"too tall":        func(d *Display) { d.Width, d.Height = 8192, 8192+8 },
		"short side 300":  func(d *Display) { d.Width, d.Height = 300, 1200 },
		"long side 600":   func(d *Display) { d.Width, d.Height = 600, 400 },
		"refresh 0":       func(d *Display) { d.RefreshHz = 0 },
		"refresh 300":     func(d *Display) { d.RefreshHz = 300 },
		"no serial":       func(d *Display) { d.Serial = 0 },
		"bad trigger":     func(d *Display) { d.Trigger = "sometimes" },
		"HiDPI 2:1 small": func(d *Display) { d.AspectW, d.AspectH, d.Width, d.Height = 2, 1, 1024, 512 },
	}
	for name, mutate := range bad {
		d := base
		mutate(&d)
		if d.Validate() == nil {
			t.Errorf("%s: Validate accepted %+v", name, d)
		}
	}
	good := map[string]func(*Display){
		"144 Hz":             func(d *Display) { d.RefreshHz = 144 },
		"portrait 1:2":       func(d *Display) { d.AspectW, d.AspectH, d.Width, d.Height = 1, 2, 540, 1080 },
		"landscape 2:1":      func(d *Display) { d.AspectW, d.AspectH, d.Width, d.Height = 2, 1, 1080, 540 },
		"1:2 without HiDPI":  func(d *Display) { d.AspectW, d.AspectH, d.Width, d.Height, d.HiDPI = 1, 2, 512, 1024, false },
		"largest":            func(d *Display) { d.Width, d.Height = MaxPoints, MaxPoints },
		"smallest, standing": func(d *Display) { d.AspectW, d.AspectH, d.Width, d.Height, d.HiDPI = 1, 2, 320, 640, false },
	}
	for name, mutate := range good {
		d := base
		mutate(&d)
		if err := d.Validate(); err != nil {
			t.Errorf("%s rejected: %v", name, err)
		}
	}
}

func TestEveryOfferedSizeIsValid(t *testing.T) {
	ratios := append([]Aspect{{3, 4}, {9, 16}, {1, 4}, {4, 1}, {3, 10}, {10, 3}, {2420, 1668}}, Presets...)
	for _, a := range ratios {
		for _, hidpi := range []bool{true, false} {
			d := NewDisplay("x", a)
			d.HiDPI = hidpi
			if err := d.Validate(); err != nil {
				t.Errorf("%v default size %d×%d: %v", a, d.Width, d.Height, err)
			}
			for _, s := range a.Sizes(hidpi) {
				d.Width, d.Height = s.W, s.H
				if err := d.Validate(); err != nil {
					t.Errorf("%v offers %v: %v", a, s, err)
				}
			}
		}
	}
}

func TestLoadDamaged(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"displays":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); !errors.Is(err, ErrDamaged) {
		t.Fatalf("Load of a damaged file gave %v", err)
	}
	kept, err := SetAside(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the damaged file is still at %s", path)
	}
	if b, err := os.ReadFile(filepath.Clean(kept)); err != nil || string(b) != `{"displays":` {
		t.Errorf("kept %s: %q, %v", kept, b, err)
	}
	if filepath.Dir(kept) != filepath.Dir(path) || filepath.Ext(kept) != ".json" {
		t.Errorf("kept as %s", kept)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.json")
	c := Default()
	extra := NewDisplay("Wide", Presets[2])
	extra.Mounted = true
	c.Displays = append(c.Displays, extra)

	if err := Save(path, c); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got.Displays, c.Displays) || got.UU != c.UU {
		t.Errorf("round trip changed the config\n got %+v\nwant %+v", got, c)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("save left %d files behind", len(entries))
	}
}

func TestLoadMissingIsDefault(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Displays) != 1 || c.Displays[0].Name != "iPad" {
		t.Errorf("missing file did not give the default: %+v", c)
	}
}

func TestLoadFillsGaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	raw := `{"displays":[{"name":"Old","aspectW":16,"aspectH":9,"width":1920,"height":1080}]}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	d := c.Displays[0]
	if d.ID == "" || d.Serial == 0 || d.RefreshHz != 60 || d.Trigger != TriggerManual {
		t.Errorf("gaps not filled: %+v", d)
	}
	if c.UU != DefaultUURule() {
		t.Errorf("UU rule not defaulted: %+v", c.UU)
	}
	if err := d.Validate(); err != nil {
		t.Error(err)
	}
}

func TestDisplayLookup(t *testing.T) {
	c := Default()
	id := c.Displays[0].ID
	d, ok := c.Display(id)
	if !ok {
		t.Fatal("display not found")
	}
	d.Name = "Renamed"
	if c.Displays[0].Name != "Renamed" {
		t.Error("Display did not return a pointer into the config")
	}
	if _, ok := c.Display("nope"); ok {
		t.Error("found a display that does not exist")
	}
}

func TestHiDPIThreshold(t *testing.T) {
	cases := []struct {
		w, h int
		ok   bool
	}{
		{800, 525, true}, {525, 800, true}, {800, 524, false}, {799, 540, false},
		{1024, 439, false}, {540, 950, true}, {2048, 1536, true},
	}
	for _, c := range cases {
		if got := HiDPIOK(c.w, c.h); got != c.ok {
			t.Errorf("HiDPIOK(%d, %d) = %v, want %v", c.w, c.h, got, c.ok)
		}
	}
	d := NewDisplay("Wide", Aspect{W: 21, H: 9})
	d.Width, d.Height = 1024, 439
	if d.Check() != BadHiDPI {
		t.Errorf("HiDPI 1024×439 check = %v, want BadHiDPI", d.Check())
	}
	d.HiDPI = false
	if d.Check() != NoProblem {
		t.Errorf("non-HiDPI 1024×439 check = %v, want NoProblem", d.Check())
	}
}

func TestCompactSize(t *testing.T) {
	cases := map[Aspect]Size{
		{W: 16, H: 9}:    {960, 540},
		{W: 220, H: 387}: {540, 950},
	}
	for a, want := range cases {
		if got := a.Sizes(true); len(got) == 0 || got[0] != want {
			t.Errorf("%v first size = %v, want %v", a, got, want)
		}
	}
	if got := (Aspect{W: 4, H: 3}).Sizes(true)[0]; got != (Size{1024, 768}) {
		t.Errorf("4:3 first size = %v, want 1024×768", got)
	}
}
