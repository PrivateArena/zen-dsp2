package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include <pipewire/pipewire.h>
#include <pipewire/filter.h>
#include <pipewire/thread-loop.h>
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

static inline struct pw_properties *make_props(const char *s) {
    return pw_properties_new_string(s);
}

void *add_input_port(struct pw_filter *filter, const char *port_name) {
    struct pw_properties *props = pw_properties_new(
        "port.name", port_name, NULL);
    return pw_filter_add_port(filter, PW_DIRECTION_INPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, props, NULL, 0);
}

void *add_output_port(struct pw_filter *filter, const char *port_name) {
    struct pw_properties *props = pw_properties_new(
        "port.name", port_name,
        "audio.format", "F32",
        "audio.channels", "1",
        NULL);
    if (strstr(port_name, "FR") != NULL)
        pw_properties_set(props, "audio.position", "FR");
    else
        pw_properties_set(props, "audio.position", "FL");

    return pw_filter_add_port(filter, PW_DIRECTION_OUTPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, props, NULL, 0);
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

var threadLoop *C.struct_pw_thread_loop

func SetupFilter() error {
	C.pw_init(nil, nil)

	tl := C.pw_thread_loop_new(C.CString("zen-dsp-eq-loop"), nil)
	if tl == nil {
		return fmt.Errorf("pw_thread_loop_new failed")
	}
	threadLoop = tl

	loop := C.pw_thread_loop_get_loop(tl)

	if ret := C.pw_thread_loop_start(tl); ret < 0 {
		return fmt.Errorf("pw_thread_loop_start failed: %d", ret)
	}

	C.pw_thread_loop_lock(tl)

	ps := C.CString("media.type=Audio,media.category=Filter," +
		"media.role=DSP,node.name=zen-dsp-eq," +
		"node.description=ZenDSPEqualizer")
	props := C.make_props(ps)
	C.free(unsafe.Pointer(ps))
	if props == nil {
		C.pw_thread_loop_unlock(tl)
		return fmt.Errorf("pw_properties_new failed")
	}

	filterName := C.CString("zen-dsp-eq")
	filter := C.pw_filter_new_simple(
		loop,
		filterName,
		props,
		&C.filter_events,
		nil,
	)
	C.free(unsafe.Pointer(filterName))
	if filter == nil {
		C.pw_thread_loop_unlock(tl)
		return fmt.Errorf("pw_filter_new_simple failed")
	}

	labels := [...]string{"FL", "FR"}
	for c := 0; c < NumChannels; c++ {
		inName := C.CString("Input_" + labels[c])
		inPorts[c] = unsafe.Pointer(C.add_input_port(filter, inName))
		C.free(unsafe.Pointer(inName))
		if inPorts[c] == nil {
			C.pw_thread_loop_unlock(tl)
			return fmt.Errorf("add_input_port %s failed", labels[c])
		}

		outName := C.CString("Output_" + labels[c])
		outPorts[c] = unsafe.Pointer(C.add_output_port(filter, outName))
		C.free(unsafe.Pointer(outName))
		if outPorts[c] == nil {
			C.pw_thread_loop_unlock(tl)
			return fmt.Errorf("add_output_port %s failed", labels[c])
		}
	}

	ret := C.pw_filter_connect(filter,
		C.PW_FILTER_FLAG_RT_PROCESS,
		nil, 0)
	if ret < 0 {
		C.pw_thread_loop_unlock(tl)
		return fmt.Errorf("pw_filter_connect failed: %d", ret)
	}

	C.pw_thread_loop_unlock(tl)

	fmt.Println("[eqd] PipeWire filter connected as 'zen-dsp-eq'")

	go SetupRouting()

	return nil
}
