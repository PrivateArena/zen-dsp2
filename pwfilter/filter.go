package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include "pw_helpers.h"
*/
import "C"

import (
	"fmt"
	"log"
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

var processCount int
var LastState string

func filterStateName(s int) string {
	switch s {
	case 0:
		return "error"
	case 1:
		return "unconnected"
	case 2:
		return "connecting"
	case 3:
		return "paused"
	case 4:
		return "streaming"
	default:
		return fmt.Sprintf("unknown(%d)", s)
	}
}

func processNative(position *C.struct_spa_io_position) {
	n := int(position.clock.duration)
	if n == 0 {
		return
	}

	processCount++
	if processCount <= 20 || processCount%1000 == 0 {
		log.Printf("[dsp] goOnProcess #%d n=%d", processCount, n)
	}

	var inBufs, outBufs [NumChannels][]float32
	allOK := true

	for c := range NumChannels {
		inPtr := C.pw_filter_get_dsp_buffer(C.zen_get_port(C.int(c)), C.uint32_t(n))
		outPtr := C.pw_filter_get_dsp_buffer(C.zen_get_port(C.int(c+2)), C.uint32_t(n))
		if inPtr == nil || outPtr == nil {
			if processCount <= 20 {
				log.Printf("[dsp] port %d nil ptr: in=%v out=%v", c, inPtr, outPtr)
			}
			allOK = false
			continue
		}
		inBufs[c] = unsafe.Slice((*float32)(inPtr), n)
		outBufs[c] = unsafe.Slice((*float32)(outPtr), n)
	}

	if !allOK {
		for c := range NumChannels {
			if inBufs[c] != nil && outBufs[c] != nil {
				copy(outBufs[c], inBufs[c])
			}
		}
		return
	}

	processAudio(inBufs, outBufs, n)
}

func stateChangedNative(old, now int, cerr *C.char) {
	oldS := filterStateName(old)
	newS := filterStateName(now)
	LastState = newS
	if C.enum_pw_filter_state(now) == C.PW_FILTER_STATE_ERROR {
		errMsg := C.GoString(cerr)
		log.Printf("[dsp] STATE: %s -> %s error=%s", oldS, newS, errMsg)
		select {
		case stateErrCh <- errMsg:
		default:
		}
	} else {
		log.Printf("[dsp] STATE: %s -> %s", oldS, newS)
	}
	select {
	case stateChangeCh <- filterState{old: old, now: now}:
	default:
	}
}

//export goOnProcess
func goOnProcess(_ unsafe.Pointer, position *C.struct_spa_io_position) {
	processNative(position)
}

//export goOnStateChanged
func goOnStateChanged(_ unsafe.Pointer, old, now C.enum_pw_filter_state, cerr *C.char) {
	stateChangedNative(int(old), int(now), cerr)
}

type filterState struct {
	old, now int
	err      string
}

var (
	stateErrCh    = make(chan string, 1)
	stateChangeCh = make(chan filterState, 4)
)
