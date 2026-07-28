package gui

import (
	"fmt"
	"image/color"
	"math"
	"sync"

	"gioui.org/app"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"

	"github.com/jang/zen-dsp2/pwfilter"
)

type Band struct {
	freq   float64
	gainDB float64
	slider widget.Float
	label  string
}

type GUI struct {
	win       *app.Window
	th        *material.Theme
	bands     [pwfilter.NumBands]Band
	bypassBtn widget.Clickable
	bypassed  bool
	statusTxt string

	mu    sync.Mutex
	dirty bool

	ops op.Ops
}

func New() *GUI {
	g := &GUI{
		win: new(app.Window),
		th:  material.NewTheme(),
	}
	g.win.Option(app.Title("Zen DSP Equalizer"))
	g.win.Option(app.Size(unit.Dp(520), unit.Dp(640)))

	for i := range pwfilter.NumBands {
		f := pwfilter.DefaultFreqs[i]
		g.bands[i] = Band{
			freq:   f,
			label:  freqLabel(f),
			slider: widget.Float{Value: .5},
		}
	}
	g.publish()
	return g
}

func freqLabel(f float64) string {
	if f >= 1000 {
		return fmt.Sprintf("%.0fk", f/1000)
	}
	return fmt.Sprintf("%.0f", f)
}

func (g *GUI) EventLoop() error {
	for {
		e := g.win.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&g.ops, e)
			g.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (g *GUI) SetStatus(s string) {
	g.mu.Lock()
	g.statusTxt = s
	g.mu.Unlock()
	g.win.Invalidate()
}

func (g *GUI) layout(gtx layout.Context) layout.Dimensions {
	for g.bypassBtn.Clicked(gtx) {
		g.bypassed = !g.bypassed
		g.publish()
	}

	g.mu.Lock()
	changed := false
	for i := range pwfilter.NumBands {
		if g.bands[i].slider.Update(gtx) {
			g.bands[i].gainDB = sliderToDB(g.bands[i].slider.Value)
			changed = true
		}
	}
	if changed || g.dirty {
		g.dirty = false
		g.mu.Unlock()
		g.publish()
	} else {
		g.mu.Unlock()
	}

	inset := layout.UniformInset(unit.Dp(12))
	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.H4(g.th, "Equalizer").Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(g.layoutSliders),
			layout.Rigid(layout.Spacer{Height: unit.Dp(8)}.Layout),
			layout.Rigid(g.layoutControls),
			layout.Rigid(layout.Spacer{Height: unit.Dp(4)}.Layout),
			layout.Rigid(g.layoutStatus),
		)
	})
}

func (g *GUI) layoutSliders(gtx layout.Context) layout.Dimensions {
	var dims layout.Dimensions
	for i := range pwfilter.NumBands {
		dims = g.layoutBand(gtx, i)
	}
	return dims
}

func (g *GUI) layoutBand(gtx layout.Context, idx int) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(48)
			return layout.Inset{Right: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				lb := material.Body2(g.th, g.bands[idx].label)
				lb.TextSize = unit.Sp(11)
				return lb.Layout(gtx)
			})
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			s := material.Slider(g.th, &g.bands[idx].slider)
			s.Color = color.NRGBA{R: 0x33, G: 0x99, B: 0xFF, A: 255}
			return s.Layout(gtx)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			gtx.Constraints.Min.X = gtx.Dp(40)
			return layout.Inset{Left: unit.Dp(8)}.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				db := sliderToDB(g.bands[idx].slider.Value)
				lb := material.Body2(g.th, dbLabel(db))
				lb.TextSize = unit.Sp(11)
				if db > 3 {
					lb.Color = color.NRGBA{R: 0xCC, G: 0x44, B: 0x44, A: 255}
				} else if db < -3 {
					lb.Color = color.NRGBA{R: 0x44, G: 0x99, B: 0xCC, A: 255}
				}
				return lb.Layout(gtx)
			})
		}),
	)
}

func (g *GUI) layoutControls(gtx layout.Context) layout.Dimensions {
	var label string
	if g.bypassed {
		label = "BYPASSED"
	} else {
		label = "Active"
	}
	btn := material.Button(g.th, &g.bypassBtn, label)
	if g.bypassed {
		btn.Background = color.NRGBA{R: 0xCC, G: 0x44, B: 0x44, A: 255}
	} else {
		btn.Background = color.NRGBA{R: 0x33, G: 0x99, B: 0xFF, A: 255}
	}
	return btn.Layout(gtx)
}

func (g *GUI) layoutStatus(gtx layout.Context) layout.Dimensions {
	g.mu.Lock()
	txt := g.statusTxt
	g.mu.Unlock()
	lb := material.Body2(g.th, txt)
	lb.Color = color.NRGBA{R: 0x88, G: 0x88, B: 0x88, A: 255}
	lb.TextSize = unit.Sp(11)
	return lb.Layout(gtx)
}

func (g *GUI) publish() {
	if g.bypassed {
		pwfilter.PublishCurve(nil)
		return
	}
	var gains [pwfilter.NumBands]float64
	for i := range pwfilter.NumBands {
		gains[i] = sliderToDB(g.bands[i].slider.Value)
	}
	c := pwfilter.ComputeCoeffs(gains, 48000, pwfilter.DefaultFreqs)
	pwfilter.PublishCurve(c)
}

func (g *GUI) Gains() [pwfilter.NumBands]float64 {
	g.mu.Lock()
	defer g.mu.Unlock()
	var gv [pwfilter.NumBands]float64
	for i := range pwfilter.NumBands {
		gv[i] = sliderToDB(g.bands[i].slider.Value)
	}
	return gv
}

func (g *GUI) SetGains(gains [pwfilter.NumBands]float64) {
	g.mu.Lock()
	for i, v := range gains {
		g.bands[i].gainDB = v
		g.bands[i].slider.Value = float32(clamp((v+12)/24, 0, 1))
	}
	g.dirty = true
	g.mu.Unlock()
}

func sliderToDB(v float32) float64 {
	return 24*float64(v) - 12
}

func dbLabel(db float64) string {
	if db > 0 {
		return fmt.Sprintf("+%.0f", math.Round(db))
	}
	if db < 0 {
		return fmt.Sprintf("%.0f", math.Round(db))
	}
	return " 0"
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
