package config

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"uuid"
)

type Trigger string

const (
	TriggerManual Trigger = "manual"

	TriggerUU Trigger = "uu"
)

const (
	MinPoints      = 640
	MinShortPoints = 320
	MaxPoints      = 8192
	MinRefresh     = 1
	MaxRefresh     = 240
)

var ErrDamaged = errors.New("the settings file is damaged")

type Display struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Device string `json:"device,omitzero"`

	AspectW int `json:"aspectW"`
	AspectH int `json:"aspectH"`

	Width  int `json:"width"`
	Height int `json:"height"`

	HiDPI     bool    `json:"hidpi,omitzero"`
	RefreshHz int     `json:"refreshHz"`
	Serial    uint32  `json:"serial"`
	Trigger   Trigger `json:"trigger"`

	ScreensOff bool `json:"screensOff,omitzero"`

	Mounted bool `json:"mounted,omitzero"`
}

type UURule struct {
	Process   string `json:"process"`
	Assertion string `json:"assertion"`

	GraceSeconds int `json:"graceSeconds"`
}

type Config struct {
	Displays []Display `json:"displays"`
	UU       UURule    `json:"uu"`
}

func DefaultUURule() UURule {
	return UURule{
		Process:      "UURemoteServer",
		Assertion:    "UURemote Disable Display Sleep",
		GraceSeconds: 5,
	}
}

const DefaultDevice = "Apple iPad Pro 13-inch (M5)"

func Default() Config {
	ipad := NewDisplay("iPad", Presets[0])
	ipad.Device = DefaultDevice
	ipad.Width, ipad.Height = 2048, 1536
	ipad.Trigger = TriggerUU
	return Config{Displays: []Display{ipad}, UU: DefaultUURule()}
}

func NewDisplay(name string, a Aspect) Display {
	s := a.DefaultSize()
	return Display{
		ID:        uuid.New().String(),
		Name:      name,
		AspectW:   a.W,
		AspectH:   a.H,
		Width:     s.W,
		Height:    s.H,
		HiDPI:     true,
		RefreshHz: 60,
		Serial:    NewSerial(),
		Trigger:   TriggerManual,
	}
}

func NewSerial() uint32 {
	for {
		var b [4]byte
		_, _ = rand.Read(b[:])
		if s := binary.BigEndian.Uint32(b[:]); s != 0 {
			return s
		}
	}
}

func (d Display) PixelSize() (w, h int) {
	if d.HiDPI {
		return d.Width * 2, d.Height * 2
	}
	return d.Width, d.Height
}

func (d Display) Label() string {
	return fmt.Sprintf("%s · %d×%d · %d:%d", d.Name, d.Width, d.Height, d.AspectW, d.AspectH)
}

type Problem int

const (
	NoProblem Problem = iota
	BadID
	BadName
	BadAspect
	BadSize
	BadHiDPI
	BadRefresh
	BadSerial
	BadTrigger
)

func sizeOK(w, h int) bool {
	long, short := max(w, h), min(w, h)
	return short >= MinShortPoints && long >= MinPoints && long <= MaxPoints
}

func (d Display) Check() Problem {
	switch {
	case d.ID == "":
		return BadID
	case d.Name == "":
		return BadName
	case !d.AspectOf().Valid():
		return BadAspect
	case !sizeOK(d.Width, d.Height):
		return BadSize
	case d.HiDPI && !HiDPIOK(d.Width, d.Height):
		return BadHiDPI
	case d.RefreshHz < MinRefresh || d.RefreshHz > MaxRefresh:
		return BadRefresh
	case d.Serial == 0:
		return BadSerial
	case d.Trigger != TriggerManual && d.Trigger != TriggerUU:
		return BadTrigger
	}
	return NoProblem
}

func (d Display) Validate() error {
	switch d.Check() {
	case BadID:
		return errors.New("display has no ID")
	case BadName:
		return errors.New("name is empty")
	case BadAspect:
		return fmt.Errorf("aspect ratio %d:%d is not between 1:%d and %d:1", d.AspectW, d.AspectH, MaxRatio, MaxRatio)
	case BadSize:
		return fmt.Errorf("size %d×%d is not at least %d×%d points, either way round, with no side over %d",
			d.Width, d.Height, MinPoints, MinShortPoints, MaxPoints)
	case BadHiDPI:
		return fmt.Errorf("HiDPI needs at least %d×%d points, either way round, not %d×%d",
			MinHiDPILong, MinHiDPIShort, d.Width, d.Height)
	case BadRefresh:
		return fmt.Errorf("refresh rate %d Hz is outside %d–%d Hz", d.RefreshHz, MinRefresh, MaxRefresh)
	case BadSerial:
		return errors.New("serial number is zero")
	case BadTrigger:
		return fmt.Errorf("unknown trigger %q", d.Trigger)
	}
	return nil
}

func (c *Config) Display(id string) (*Display, bool) {
	for i := range c.Displays {
		if c.Displays[i].ID == id {
			return &c.Displays[i], true
		}
	}
	return nil, false
}

func (c *Config) normalize() {
	def := DefaultUURule()
	if c.UU.Process == "" {
		c.UU.Process = def.Process
	}
	if c.UU.Assertion == "" {
		c.UU.Assertion = def.Assertion
	}
	if c.UU.GraceSeconds <= 0 {
		c.UU.GraceSeconds = def.GraceSeconds
	}
	for i := range c.Displays {
		d := &c.Displays[i]
		if d.ID == "" {
			d.ID = uuid.New().String()
		}
		if d.Serial == 0 {
			d.Serial = NewSerial()
		}
		if d.RefreshHz == 0 {
			d.RefreshHz = 60
		}
		if d.Trigger == "" {
			d.Trigger = TriggerManual
		}
	}
}

func Path(appName string) (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, appName, "config.json"), nil
}

func Load(path string) (Config, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return Default(), nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := json.UnmarshalRead(bytes.NewReader(data), &c); err != nil {
		return Config{}, fmt.Errorf("%w: %s: %w", ErrDamaged, path, err)
	}
	c.normalize()
	return c, nil
}

func SetAside(path string) (string, error) {
	ext := filepath.Ext(path)
	kept := strings.TrimSuffix(path, ext) + "-damaged-" + time.Now().Format("20060102-150405") + ext
	return kept, os.Rename(path, kept)
}

func Save(path string, c Config) error {
	data, err := json.Marshal(c, jsontext.WithIndent("  "))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".config-*.json")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
