package pwfilter

import (
	"log"
	"math"
)

var DefaultFreqs = [NumBands]float64{
	31, 62, 125, 250, 500, 1000, 2000, 4000, 8000, 16000,
}

func ComputeCoeffs(gainsDB [NumBands]float64, sampleRate float64, freqs [NumBands]float64) *Curve {
	c := &Curve{}
	q := 1.414

	for i := range NumBands {
		g := gainsDB[i]
		if g == 0 {
			c.Bands[i] = BandCoeffs{B0: 1, B1: 0, B2: 0, A1: 0, A2: 0}
			continue
		}

		w0 := 2 * math.Pi * freqs[i] / sampleRate
		sinW0 := math.Sin(w0)
		cosW0 := math.Cos(w0)
		alpha := sinW0 / (2 * q)
		a := math.Pow(10, g/40)

		b0 := 1 + alpha*a
		b1 := -2 * cosW0
		b2 := 1 - alpha*a
		a0 := 1 + alpha/a
		a1 := -2 * cosW0
		a2 := 1 - alpha/a

		invA0 := 1 / a0
		c.Bands[i] = BandCoeffs{
			B0: float32(b0 * invA0),
			B1: float32(b1 * invA0),
			B2: float32(b2 * invA0),
			A1: float32(a1 * invA0),
			A2: float32(a2 * invA0),
		}
	}
	log.Printf("[eqd] coeffs: gains=%v rate=%.0f band0=%+v", gainsDB, sampleRate, c.Bands[0])
	return c
}
