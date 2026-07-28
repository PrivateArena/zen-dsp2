package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include <pipewire/pipewire.h>
#include <pipewire/filter.h>

extern void goOnProcess(void *userdata, struct spa_io_position *position);
extern void goOnStateChanged(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, char *error);
*/
import "C"

import (
	"sync/atomic"
	"unsafe"
)

const NumBands = 10
const NumChannels = 2

type BandCoeffs struct{ B0, B1, B2, A1, A2 float32 }

type Curve struct{ Bands [NumBands]BandCoeffs }

var current atomic.Pointer[Curve]

func PublishCurve(c *Curve) { current.Store(c) }

func LoadCurve() *Curve { return current.Load() }

type runningState struct {
	z1 [NumChannels][NumBands]float32
	z2 [NumChannels][NumBands]float32
}

var rt runningState

func processAudio(inBufs, outBufs [NumChannels][]float32, n int) {
	curve := current.Load()
	if curve == nil {
		for c := range NumChannels {
			copy(outBufs[c], inBufs[c])
		}
		return
	}

	for i := range n {
		for c := range NumChannels {
			y := inBufs[c][i]
			for b := range curve.Bands {
				coeff := &curve.Bands[b]
				v := coeff.B0*y + rt.z1[c][b]
				rt.z1[c][b] = coeff.B1*y - coeff.A1*v + rt.z2[c][b]
				rt.z2[c][b] = coeff.B2*y - coeff.A2*v
				y = v
			}
			outBufs[c][i] = y
		}
	}
}

//export goOnProcess
func goOnProcess(_ unsafe.Pointer, position *C.struct_spa_io_position) {
	n := int(position.clock.duration)
	if n == 0 {
		return
	}

	var inBufs, outBufs [NumChannels][]float32

	for c := range NumChannels {
		inPtr := C.pw_filter_get_dsp_buffer(inPorts[c], C.uint32_t(n))
		outPtr := C.pw_filter_get_dsp_buffer(outPorts[c], C.uint32_t(n))
		if inPtr == nil || outPtr == nil {
			return
		}
		inBufs[c] = unsafe.Slice((*float32)(inPtr), n)
		outBufs[c] = unsafe.Slice((*float32)(outPtr), n)
	}

	processAudio(inBufs, outBufs, n)
}

//export goOnStateChanged
func goOnStateChanged(_ unsafe.Pointer, old, now C.enum_pw_filter_state, cerr *C.char) {
	if now == C.PW_FILTER_STATE_ERROR {
		select {
		case stateErrCh <- C.GoString(cerr):
		default:
		}
	}
	select {
	case stateChangeCh <- filterState{old: int(old), now: int(now)}:
	default:
	}
}

type filterState struct {
	old, now int
	err      string
}

var (
	inPorts  [NumChannels]unsafe.Pointer
	outPorts [NumChannels]unsafe.Pointer
	stateErrCh      = make(chan string, 1)
	stateChangeCh   = make(chan filterState, 4)
)
