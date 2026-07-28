package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include "pw_helpers.h"
*/
import "C"

import (
	"log"
	"os/exec"
	"strings"
	"time"
)

var filterNodeID uint32
var sinkNodeID uint32
var sinkNodeSerial uint64

func SetupRouting() {
	time.Sleep(1 * time.Second)

	waitForNode("zen-dsp-sink")
	sink, ok := findNodeByName("zen-dsp-sink")
	if !ok {
		log.Printf("[rt] CRITICAL: zen-dsp-sink not found!")
		return
	}
	sinkNodeID = sink.id
	sinkNodeSerial = sink.serial
	log.Printf("[rt] found null sink: id=%d serial=%d", sinkNodeID, sinkNodeSerial)

	hwName := findHardwareSinkPactl()
	if hwName == "" {
		log.Printf("[rt] no hardware sink found via pactl, trying registry")
		sinks := findNodesByClass("Audio/Sink")
		for _, s := range sinks {
			if s.name != "zen-dsp-sink" {
				hwName = s.name
				break
			}
		}
	}
	if hwName == "" {
		log.Printf("[rt] CRITICAL: no hardware sink found!")
		return
	}
	log.Printf("[rt] hardware sink: %s", hwName)

	waitForNode(hwName)
	hw, ok := findNodeByName(hwName)
	if !ok {
		log.Printf("[rt] CRITICAL: hw sink %s not found in registry!", hwName)
		return
	}
	log.Printf("[rt] found hw sink: id=%d serial=%d", hw.id, hw.serial)

	waitForNode("zen-dsp-eq")
	filter, ok := findNodeByName("zen-dsp-eq")
	if !ok {
		log.Printf("[rt] CRITICAL: zen-dsp-eq not found!")
		return
	}
	log.Printf("[rt] found filter: id=%d serial=%d", filter.id, filter.serial)

	// Link null_sink -> filter
	log.Printf("[rt] linking zen-dsp-sink(id=%d) -> zen-dsp-eq(id=%d)", sink.id, filter.id)
	time.Sleep(300 * time.Millisecond)
	linkRet := int(C.zen_link_nodes_by_id(C.uint32_t(sink.id), C.uint32_t(filter.id)))
	log.Printf("[rt] null_sink -> filter: %d links", linkRet)

	time.Sleep(500 * time.Millisecond)

	// Link filter -> hw_sink
	log.Printf("[rt] linking zen-dsp-eq(id=%d) -> %s(id=%d)", filter.id, hwName, hw.id)
	linkRet = int(C.zen_link_nodes_by_id(C.uint32_t(filter.id), C.uint32_t(hw.id)))
	log.Printf("[rt] filter -> hw_sink: %d links", linkRet)

	time.Sleep(500 * time.Millisecond)
	go routeStreamsLoop()
}

func waitForNode(name string) {
	for i := 0; i < 200; i++ {
		if _, ok := findNodeByName(name); ok {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Printf("[rt] WARNING: node '%s' not found after 10s", name)
}

func routeStreamsLoop() {
	routed := make(map[uint32]bool)
	for {
		time.Sleep(2 * time.Second)

		if int(C.zen_has_metadata()) == 0 {
			continue
		}

		nodesMu.Lock()
		var toRoute []nodeInfo
		for _, n := range nodes {
			if n.class == "Stream/Output/Audio" && !routed[n.id] {
				toRoute = append(toRoute, n)
			}
		}
		nodesMu.Unlock()

		for _, n := range toRoute {
			log.Printf("[rt] routing stream '%s'(id=%d serial=%d) -> zen-dsp-sink", n.name, n.id, n.serial)
			ret := int(C.zen_set_stream_target(
				C.uint32_t(n.id),
				C.uint32_t(sinkNodeID),
				C.uint64_t(sinkNodeSerial),
			))
			if ret == 0 {
				routed[n.id] = true
				log.Printf("[rt] stream %s routed to zen-dsp-sink", n.name)
			} else {
				log.Printf("[rt] failed to route stream %s", n.name)
			}
		}
	}
}

func findHardwareSinkPactl() string {
	out, err := run("pactl", "list", "sinks")
	if err != nil {
		return ""
	}

	var running, idle, any string
	blocks := strings.Split(out, "\n\n")
	for _, block := range blocks {
		if !strings.Contains(block, "Name:") {
			continue
		}
		lines := strings.Split(block, "\n")
		var name, state string
		isVirtual := false
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "Name:") {
				name = strings.TrimSpace(l[5:])
			}
			if strings.HasPrefix(l, "State:") {
				state = strings.TrimSpace(l[6:])
			}
			if strings.HasPrefix(l, "node.virtual") && strings.Contains(l, "true") {
				isVirtual = true
			}
		}
		if name == "" || isVirtual || strings.Contains(name, "zen-dsp") {
			continue
		}
		if any == "" {
			any = name
		}
		if state == "RUNNING" && running == "" {
			running = name
		}
		if state == "IDLE" && idle == "" {
			idle = name
		}
	}
	if running != "" {
		return running
	}
	if idle != "" {
		return idle
	}
	return any
}

func run(cmd string, args ...string) (string, error) {
	out, err := exec.Command(cmd, args...).CombinedOutput()
	return string(out), err
}
