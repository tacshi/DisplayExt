package vdisplay

import (
	"fmt"
	"math"
	"time"

	"displayext/internal/config"
)

const (
	VendorID  = 0x4458
	ProductID = 0x0001
)

const pointsPerInch = 110

type Spec struct {
	Name string `json:"name"`

	PointsW int  `json:"pointsW"`
	PointsH int  `json:"pointsH"`
	HiDPI   bool `json:"hidpi,omitzero"`

	PixelsW   int           `json:"pixelsW"`
	PixelsH   int           `json:"pixelsH"`
	RefreshHz int           `json:"refreshHz"`
	Serial    uint32        `json:"serial"`
	Extra     []config.Size `json:"extra"`
	SizeMMW   float64       `json:"sizeMMW"`
	SizeMMH   float64       `json:"sizeMMH"`
}

func FromConfig(d config.Display) Spec {
	pw, ph := d.PixelSize()
	return Spec{
		Name:      d.Name,
		PointsW:   d.Width,
		PointsH:   d.Height,
		HiDPI:     d.HiDPI,
		PixelsW:   pw,
		PixelsH:   ph,
		RefreshHz: d.RefreshHz,
		Serial:    d.Serial,
		Extra:     d.ExtraModes(),
		SizeMMW:   float64(d.Width) * 25.4 / pointsPerInch,
		SizeMMH:   float64(d.Height) * 25.4 / pointsPerInch,
	}
}

type Options struct {
	PrivateQueue bool `json:"privateQueue,omitzero"`

	NoColor bool `json:"noColor,omitzero"`

	TransferFunction int `json:"transferFunction,omitzero"`

	ActiveTimeout time.Duration `json:"activeTimeout,omitzero"`
}

var DefaultOptions = Options{}

type Mode struct {
	PointsW, PointsH int
	PixelsW, PixelsH int
	RefreshHz        float64
}

func (m Mode) String() string {
	return fmt.Sprintf("%d×%d (%d×%d px) @%gHz", m.PointsW, m.PointsH, m.PixelsW, m.PixelsH, m.RefreshHz)
}

func (m Mode) Same(o Mode) bool {
	return m.PointsW == o.PointsW && m.PointsH == o.PointsH &&
		m.PixelsW == o.PixelsW && m.PixelsH == o.PixelsH &&
		math.Abs(m.RefreshHz-o.RefreshHz) < 0.5
}
