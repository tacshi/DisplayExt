package devices

import (
	"testing"

	"displayext/internal/config"
)

func TestCatalogLoads(t *testing.T) {
	n, tablets := 0, 0
	for _, b := range Brands() {
		for _, m := range b.Models {
			n++
			if m.Kind == Tablet {
				tablets++
			}
			if m.Brand != b.Name {
				t.Errorf("%s has brand %q", m.ID(), m.Brand)
			}
			if got, ok := Lookup(m.ID()); !ok || got != m {
				t.Errorf("Lookup(%q) = %+v, %v", m.ID(), got, ok)
			}
			for _, portrait := range []bool{true, false} {
				if a := m.Aspect(portrait); !a.Valid() || (a.W < a.H) != portrait && a.W != a.H {
					t.Errorf("%s portrait=%v aspect %v", m.ID(), portrait, a)
				}
			}
		}
	}
	if n != 800 || tablets < 20 {
		t.Errorf("catalog has %d models, %d tablets", n, tablets)
	}
}

func TestParseRejects(t *testing.T) {
	bad := map[string]string{
		"landscape screen": `{"brands":[{"name":"A","models":[{"name":"X","kind":"phone","screen":[2000,1000]}]}]}`,
		"duplicate model":  `{"brands":[{"name":"A","models":[{"name":"X","kind":"phone","screen":[1,2]},{"name":"x","kind":"phone","screen":[1,2]}]}]}`,
		"duplicate brand":  `{"brands":[{"name":"A","models":[{"name":"X","kind":"phone","screen":[1,2]}]},{"name":"a","models":[{"name":"Y","kind":"phone","screen":[1,2]}]}]}`,
		"unknown kind":     `{"brands":[{"name":"A","models":[{"name":"X","kind":"watch","screen":[1,2]}]}]}`,
		"tablet no points": `{"brands":[{"name":"A","models":[{"name":"X","kind":"tablet","screen":[1,2]}]}]}`,
		"no models":        `{"brands":[{"name":"A","models":[]}]}`,
	}
	for name, src := range bad {
		if _, err := parse([]byte(src)); err == nil {
			t.Errorf("%s: parse accepted it", name)
		}
	}
}

func TestDefaultDevice(t *testing.T) {
	m, ok := Lookup(config.DefaultDevice)
	if !ok {
		t.Fatalf("the default device %q is not in the catalog", config.DefaultDevice)
	}
	if a := m.Aspect(false); a != (config.Aspect{W: 4, H: 3}) {
		t.Errorf("default device landscape aspect = %v, want 4:3", a)
	}
	if s, ok := m.Native(false); !ok || s != (config.Size{W: 1376, H: 1032}) {
		t.Errorf("default device native size = %v, %v", s, ok)
	}
}

func TestPhonePortraitMatchesSnug(t *testing.T) {
	m, ok := Lookup("Apple iPhone 17 Pro Max")
	if !ok {
		t.Fatal("no iPhone 17 Pro Max")
	}
	a := m.Aspect(true)
	sizes := a.Sizes(true)
	if len(sizes) == 0 || sizes[0] != (config.Size{W: 540, H: 950}) {
		t.Errorf("iPhone 17 Pro Max portrait %v sizes %v, want 540×950 first", a, sizes)
	}
	if _, ok := m.Native(true); ok {
		t.Error("phones have no native size")
	}
	l := m.Aspect(false)
	if r := float64(l.W) / float64(l.H); r < 1.8 || r > 1.85 {
		t.Errorf("iPhone 17 Pro Max landscape ratio %.3f", r)
	}
}

func TestSearch(t *testing.T) {
	cases := map[string]string{
		"ipad pro 13":  "Apple iPad Pro 13-inch (M5)",
		"17promax":     "Apple iPhone 17 Pro Max",
		"华为 mate":      "HUAWEI Mate 70 Pro",
		"huaweimate70": "HUAWEI Mate 70 Pro",
		"苹果 iPhone 16": "Apple iPhone 16",
	}
	for q, want := range cases {
		found := false
		for _, b := range Search(q) {
			for _, m := range b.Models {
				found = found || m.ID() == want
			}
		}
		if !found {
			t.Errorf("Search(%q) does not find %s", q, want)
		}
	}
	if got := Search("nothing like this"); len(got) != 0 {
		t.Errorf("Search found %v", got)
	}
	if got := Search("  "); len(got) != len(Brands()) {
		t.Errorf("an empty search lists %d brands", len(got))
	}
}

func TestShortName(t *testing.T) {
	cases := map[string]string{
		"Apple iPad Pro 13-inch (M5)": "iPad Pro 13″",
		"Apple iPhone 17 Pro":         "iPhone 17 Pro",
		"HUAWEI Mate 70 Pro":          "HUAWEI Mate 70 Pro",
	}
	for id, want := range cases {
		m, ok := Lookup(id)
		if !ok {
			t.Fatalf("no %s", id)
		}
		if got := m.ShortName(); got != want {
			t.Errorf("%s short name %q, want %q", id, got, want)
		}
	}
}
