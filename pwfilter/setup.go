package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include <pipewire/pipewire.h>
#include <pipewire/filter.h>
#include <spa/param/audio/format-utils.h>

extern void goOnProcess(void *userdata, struct spa_io_position *position);
extern void goOnStateChanged(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, char *error);

static void on_process(void *userdata, struct spa_io_position *position) {
    goOnProcess(userdata, position);
}
static void on_state_changed(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, const char *error) {
    goOnStateChanged(userdata, old, state, (char *)error);
}

const struct pw_filter_events filter_events = {
    PW_VERSION_FILTER_EVENTS,
    .state_changed = on_state_changed,
    .process = on_process,
};
*/
import "C"

import (
	"fmt"
	"unsafe"
)

func runMainLoop(ml *C.struct_pw_main_loop) {
	C.pw_main_loop_run(ml)
}

func str(s string) *C.char {
	return C.CString(s)
}

func SetupFilter() error {
	C.pw_init(nil, nil)

	ml := C.pw_main_loop_new(nil)
	if ml == nil {
		return fmt.Errorf("pw_main_loop_new failed")
	}

	ps := str("media.type=Audio,media.category=Filter,media.role=Production,node.name=zen-dsp-eq,node.description=ZenDSPEqualizer")
	props := C.pw_properties_new_string(ps)
	C.free(unsafe.Pointer(ps))
	if props == nil {
		return fmt.Errorf("pw_properties_new_string failed")
	}

	name := str("zen-dsp-eq")
	filter := C.pw_filter_new_simple(
		C.pw_main_loop_get_loop(ml),
		name,
		props,
		&C.filter_events,
		nil,
	)
	C.free(unsafe.Pointer(name))
	if filter == nil {
		return fmt.Errorf("pw_filter_new_simple failed")
	}

	mainLoop = ml

	ips := str("format.dsp=float32,port.name=Input,audio.channels=2")
	inProps := C.pw_properties_new_string(ips)
	C.free(unsafe.Pointer(ips))

	inPort = C.pw_filter_add_port(filter, C.PW_DIRECTION_INPUT,
		C.PW_FILTER_PORT_FLAG_MAP_BUFFERS,
		0,
		inProps,
		nil, 0)
	if inPort == nil {
		return fmt.Errorf("pw_filter_add_port input failed")
	}

	ops := str("format.dsp=float32,port.name=Output,audio.channels=2")
	outProps := C.pw_properties_new_string(ops)
	C.free(unsafe.Pointer(ops))

	outPort = C.pw_filter_add_port(filter, C.PW_DIRECTION_OUTPUT,
		C.PW_FILTER_PORT_FLAG_MAP_BUFFERS,
		0,
		outProps,
		nil, 0)
	if outPort == nil {
		return fmt.Errorf("pw_filter_add_port output failed")
	}

	ret := C.pw_filter_connect(filter,
		C.PW_FILTER_FLAG_RT_PROCESS,
		nil, 0)
	if ret < 0 {
		return fmt.Errorf("pw_filter_connect failed: %d", ret)
	}

	fmt.Println("[eqd] PipeWire filter connected as 'zen-dsp-eq'")

	go runMainLoop(ml)

	return nil
}

var mainLoop *C.struct_pw_main_loop
