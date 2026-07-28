package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"

	"gioui.org/app"

	"github.com/jang/zen-dsp2/gui"
	"github.com/jang/zen-dsp2/pwfilter"
	"github.com/jang/zen-dsp2/state"
)

/*
#cgo pkg-config: libpipewire-0.3
#include <pipewire/pipewire.h>
*/
import "C"

func main() {
	log.Printf("[pw] version=%s", C.GoString(C.pw_get_library_version()))

	g := gui.New()

	if cd, err := state.Load(); err == nil {
		g.SetGains(cd.Gains)
	} else {
		g.SetStatus("no saved curve, starting flat")
	}

	if err := pwfilter.SetupFilter(); err != nil {
		g.SetStatus(fmt.Sprintf("ERROR: %v", err))
	} else {
		g.SetStatus("connected — PipeWire filter active")
	}

	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt)
		<-sig
		g.SetStatus("saving curve and exiting...")
		gains := g.Gains()
		if err := state.Save(gains); err != nil {
			log.Printf("state save: %v", err)
		}
		os.Exit(0)
	}()

	go func() {
		if err := g.EventLoop(); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()

	app.Main()
}
