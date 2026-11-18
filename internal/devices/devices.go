package devices

import (
	"bytes"
	_ "embed"
	"encoding/json/v2"
	"fmt"
	"math"
	"strings"
	"sync"

	"displayext/internal/config"
)

type Kind string

const (
	Phone  Kind = "phone"
	Tablet Kind = "tablet"
)

type Fold string

const (
	Inner Fold = "inner"
	Outer Fold = "outer"
)

const (
	AssumedPoints    = 400
	UUInsetPortrait  = 91
	UUInsetLandscape = 77
)

type Model struct {
	Brand   string `json:"-"`
	BrandZh string `json:"-"`
	Name    string `json:"name"`
	Fold    Fold   `json:"fold,omitzero"`
	Kind    Kind   `json:"kind"`
	Screen  [2]int `json:"screen"`
	Points  int    `json:"points,omitzero"`
}

type Brand struct {
	Name    string   `json:"name"`
	Zh      string   `json:"zh,omitzero"`
	Aliases []string `json:"aliases,omitzero"`
	Models  []Model  `json:"models"`
}

//go:embed devices.json
var data []byte

var catalog = sync.OnceValues(func() ([]Brand, error) { return parse(data) })

func Brands() []Brand {
	bs, err := catalog()
	if err != nil {
		panic(err)
	}
	return bs
}

func parse(b []byte) ([]Brand, error) {
	var file struct {
		Brands []Brand `json:"brands"`
	}
	if err := json.UnmarshalRead(bytes.NewReader(b), &file); err != nil {
		return nil, fmt.Errorf("devices: %w", err)
	}
	seen := map[string]bool{}
	brands := map[string]bool{}
	for i := range file.Brands {
		br := &file.Brands[i]
		if br.Name == "" || len(br.Models) == 0 {
			return nil, fmt.Errorf("devices: brand %q has no name or no models", br.Name)
		}
		if brands[normalize(br.Name)] {
			return nil, fmt.Errorf("devices: brand %q is listed twice", br.Name)
		}
		brands[normalize(br.Name)] = true
		for j := range br.Models {
			m := &br.Models[j]
			m.Brand, m.BrandZh = br.Name, br.Zh
			w, h := m.Screen[0], m.Screen[1]
			switch {
			case m.Name == "":
				return nil, fmt.Errorf("devices: %s has a model with no name", br.Name)
			case w <= 0 || h <= 0 || w > h:
				return nil, fmt.Errorf("devices: %s: the screen must be positive and portrait", m.ID())
			case m.Points < 0 || m.Points > w:
				return nil, fmt.Errorf("devices: %s: points must be between 0 and the pixel width", m.ID())
			case m.Kind != Phone && m.Kind != Tablet:
				return nil, fmt.Errorf("devices: %s: unknown kind %q", m.ID(), m.Kind)
			case m.Fold != "" && m.Fold != Inner && m.Fold != Outer:
				return nil, fmt.Errorf("devices: %s: unknown fold %q", m.ID(), m.Fold)
			case m.Kind == Tablet && m.Points == 0:
				return nil, fmt.Errorf("devices: %s: a tablet needs points", m.ID())
			}
			if seen[normalize(m.ID())] {
				return nil, fmt.Errorf("devices: %s is listed twice", m.ID())
			}
			seen[normalize(m.ID())] = true
		}
	}
	return file.Brands, nil
}

func (m Model) ID() string {
	id := m.Brand + " " + m.Name
	if m.Fold != "" {
		id += " " + string(m.Fold)
	}
	return id
}

func Lookup(id string) (Model, bool) {
	if id == "" {
		return Model{}, false
	}
	for _, b := range Brands() {
		for _, m := range b.Models {
			if m.ID() == id {
				return m, true
			}
		}
	}
	return Model{}, false
}

func Search(query string) []Brand {
	q := normalize(query)
	if q == "" {
		return Brands()
	}
	var out []Brand
	for _, b := range Brands() {
		names := []string{normalize(b.Name)}
		if b.Zh != "" {
			names = append(names, normalize(b.Zh))
		}
		for _, a := range b.Aliases {
			names = append(names, normalize(a))
		}
		var models []Model
		for _, m := range b.Models {
			name := normalize(m.Name)
			for _, n := range append([]string{""}, names...) {
				if strings.Contains(n+name, q) {
					models = append(models, m)
					break
				}
			}
		}
		if len(models) > 0 {
			nb := b
			nb.Models = models
			out = append(out, nb)
		}
	}
	return out
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), "")
}

func (m Model) points() float64 {
	if m.Points > 0 {
		return float64(m.Points)
	}
	return AssumedPoints
}

func (m Model) long() float64 {
	return float64(m.Screen[1]) * m.points() / float64(m.Screen[0])
}

func (m Model) Aspect(portrait bool) config.Aspect {
	short, long := m.points(), m.long()
	if m.Kind == Phone {
		if portrait {
			long -= 2 * UUInsetPortrait
		} else {
			long -= 2 * UUInsetLandscape
		}
		long = max(long, short)
	}
	s, l := int(math.Round(short*10)), int(math.Round(long*10))
	if m.Kind == Tablet {
		s, l = m.Screen[0], m.Screen[1]
	}
	g := gcd(s, l)
	s, l = s/g, l/g
	if portrait {
		return config.Aspect{W: s, H: l}
	}
	return config.Aspect{W: l, H: s}
}

func (m Model) Sharpest(portrait bool) config.Size {
	short := int(math.Round(float64(m.Screen[0]) / 2))
	a := m.Aspect(portrait)
	if portrait {
		return config.Size{W: short, H: int(math.Round(float64(short) * float64(a.H) / float64(a.W)))}
	}
	return config.Size{W: int(math.Round(float64(short) * float64(a.W) / float64(a.H))), H: short}
}

func (m Model) ShortName() string {
	name := m.Name
	if i := strings.Index(name, " ("); i > 0 {
		name = name[:i]
	}
	name = strings.ReplaceAll(name, "-inch", "″")
	if m.Brand != "Apple" {
		name = m.Brand + " " + name
	}
	return name
}

func gcd(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return max(a, 1)
}
