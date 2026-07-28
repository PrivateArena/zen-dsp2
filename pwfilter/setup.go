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

static const struct spa_pod *make_audio_format_pod(
    struct spa_pod_builder *b, const char *position)
{
    struct spa_audio_info_raw info = {
        .format = SPA_AUDIO_FORMAT_F32,
        .flags = 0,
        .rate = 0,
        .channels = 1,
    };
    info.position[0] = position ? SPA_AUDIO_CHANNEL_FL : SPA_AUDIO_CHANNEL_MONO;
    if (position && position[0] == 'F' && position[1] == 'R')
        info.position[0] = SPA_AUDIO_CHANNEL_FR;
    if (position && position[0] == 'M' && position[1] == 'O')
        info.position[0] = SPA_AUDIO_CHANNEL_MONO;
    return spa_format_audio_raw_build(b, SPA_PARAM_EnumFormat, &info);
}

void *add_audio_port(
    struct pw_filter *filter,
    enum pw_direction direction,
    const char *port_name,
    const char *position)
{
    uint8_t buffer[1024];
    struct spa_pod_builder b = SPA_POD_BUILDER_INIT(buffer, sizeof(buffer));
    const struct spa_pod *param = make_audio_format_pod(&b, position);
    const struct spa_pod *params[] = { param };

    struct pw_properties *props = pw_properties_new(
        "port.name", port_name, NULL);

    return pw_filter_add_port(filter, direction,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, props, params, 1);
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

	portLabels := []string{"FL", "FR"}
	for c := 0; c < NumChannels; c++ {
		pos := C.CString(portLabels[c])
		inName := C.CString("Input_" + portLabels[c])
		inPorts[c] = unsafe.Pointer(C.add_audio_port(filter,
			C.PW_DIRECTION_INPUT, inName, pos))
		C.free(unsafe.Pointer(inName))
		C.free(unsafe.Pointer(pos))
		if inPorts[c] == nil {
			C.pw_thread_loop_unlock(tl)
			return fmt.Errorf("add_audio_port input %d failed", c)
		}

		outPos := C.CString(portLabels[c])
		outName := C.CString("Output_" + portLabels[c])
		outPorts[c] = unsafe.Pointer(C.add_audio_port(filter,
			C.PW_DIRECTION_OUTPUT, outName, outPos))
		C.free(unsafe.Pointer(outName))
		C.free(unsafe.Pointer(outPos))
		if outPorts[c] == nil {
			C.pw_thread_loop_unlock(tl)
			return fmt.Errorf("add_audio_port output %d failed", c)
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
