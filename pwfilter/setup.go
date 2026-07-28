package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include "pw_helpers.h"
*/
import "C"

import (
	"fmt"
	"log"
	"sync"
	"time"
)

type nodeInfo struct {
	id     uint32
	serial uint64
	name   string
	class  string
}

var (
	nodesMu sync.Mutex
	nodes   []nodeInfo
	// Channel for C callback to safely communicate (channel is lock-free)
	nodeEvents = make(chan nodeInfo, 256)
)

//export goOnNodeGlobal
func goOnNodeGlobal(id C.uint32_t, name, mediaClass *C.char, serial C.uint64_t) {
	select {
	case nodeEvents <- nodeInfo{
		id:     uint32(id),
		serial: uint64(serial),
		name:   C.GoString(name),
		class:  C.GoString(mediaClass),
	}:
	default:
	}
}

//export goOnCoreError
func goOnCoreError(message *C.char) {
	log.Printf("[pw] core error: %s", C.GoString(message))
}

func init() {
	go func() {
		for n := range nodeEvents {
			nodesMu.Lock()
			nodes = append(nodes, n)
			nodesMu.Unlock()
		}
	}()
}

// exported for Go runtime — not used directly
var C_g struct{}

func SetupFilter() error {
	ret := int(C.zen_init())
	if ret < 0 {
		return fmt.Errorf("zen_init failed: code=%d", ret)
	}
	log.Printf("[eqd] PipeWire core initialized")

	log.Printf("[eqd] waiting for metadata...")
	waitForMetadata()

	// Create null-audio-sink via PipeWire API (like JamesDSP)
	sinkProxy := C.zen_create_sink()
	if sinkProxy == nil {
		return fmt.Errorf("zen_create_sink failed")
	}
	log.Printf("[eqd] null-audio-sink created: zen-dsp-sink")

	time.Sleep(500 * time.Millisecond)

	// Create pw_filter via pw_filter_new_simple (reliable, was working before)
	// Need to get the loop from our manager
	// pw_filter_new_simple creates its own internal context/core/loop
	// We pass our own loop only for the _simple variant event integration
	// Actually pw_filter_new_simple takes a pw_loop for the main loop
	// The simplest: pass the default main loop (NULL = use default)
	// But we don't have a default main loop.
	// Let's use the filter's own internal loop by passing NULL
	filterPtr := C.zen_create_filter()
	if filterPtr == nil {
		return fmt.Errorf("zen_create_filter failed")
	}
	log.Printf("[eqd] pw_filter created")

	ret = int(C.zen_connect_filter(filterPtr))
	if ret < 0 {
		return fmt.Errorf("zen_connect_filter failed: %d", ret)
	}
	log.Printf("[eqd] pw_filter connected")

	// Get filter node ID
	fid := uint32(C.zen_get_filter_node_id())
	log.Printf("[eqd] filter node id=%d", fid)

	go SetupRouting()

	return nil
}

func waitForMetadata() {
	for i := 0; i < 100; i++ {
		if int(C.zen_has_metadata()) != 0 {
			log.Printf("[eqd] metadata available")
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	log.Printf("[eqd] WARNING: metadata not available after 5s")
}

func findNodeByName(name string) (nodeInfo, bool) {
	nodesMu.Lock()
	defer nodesMu.Unlock()
	for _, n := range nodes {
		if n.name == name {
			return n, true
		}
	}
	return nodeInfo{}, false
}

func findNodesByClass(class string) []nodeInfo {
	nodesMu.Lock()
	defer nodesMu.Unlock()
	var result []nodeInfo
	for _, n := range nodes {
		if n.class == class {
			result = append(result, n)
		}
	}
	return result
}
