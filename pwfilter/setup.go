package pwfilter

/*
#cgo pkg-config: libpipewire-0.3
#include <pipewire/pipewire.h>
#include <pipewire/filter.h>
#include <pipewire/thread-loop.h>
#include <pipewire/extensions/metadata.h>
#include <spa/param/audio/format-utils.h>
#include <spa/utils/json.h>

extern void goOnProcess(void *userdata, struct spa_io_position *position);
extern void goOnStateChanged(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, char *error);
extern void goOnNodeGlobal(uint32_t id, const char *name,
                            const char *media_class, uint64_t serial);
extern void goOnCoreError(const char *message);

// --- CORE STATE ---
static struct pw_state {
    struct pw_thread_loop *loop;
    struct pw_context *context;
    struct pw_core *core;
    struct pw_registry *registry;
    struct pw_metadata *metadata;
    struct pw_filter *filter;
} pw = {0};

static struct spa_hook core_listener, registry_listener, metadata_listener;

// --- CALLBACKS ---

static void on_core_error(void *data, uint32_t id, int seq, int res, const char *message) {
    goOnCoreError(message);
    pw_thread_loop_signal(pw.loop, false);
}

static void on_core_done(void *data, uint32_t id, int seq) {
    pw_thread_loop_signal(pw.loop, false);
}

static void on_registry_global(void *data, uint32_t id, uint32_t permissions,
                                const char *type, uint32_t version,
                                const struct spa_dict *props) {
    if (!props) return;

    if (strcmp(type, PW_TYPE_INTERFACE_Metadata) == 0) {
        const char *mname = spa_dict_lookup(props, PW_KEY_METADATA_NAME);
        if (mname && strcmp(mname, "default") == 0) {
            // Bind the default metadata
            pw.metadata = (struct pw_metadata *)pw_registry_bind(
                pw.registry, id, type, PW_VERSION_METADATA, 0);
            if (pw.metadata) {
                // Add listener to track default sink changes
                struct pw_metadata_events mevents = {
                    PW_VERSION_METADATA_EVENTS,
                    .property = NULL, // we don't listen for changes
                };
                pw_metadata_add_listener(pw.metadata, &metadata_listener, &mevents, NULL);
            }
        }
        return;
    }

    if (strcmp(type, PW_TYPE_INTERFACE_Node) == 0) {
        const char *name = spa_dict_lookup(props, PW_KEY_NODE_NAME);
        const char *media_class = spa_dict_lookup(props, PW_KEY_MEDIA_CLASS);
        if (!name) return;

        uint64_t serial = 0;
        const char *serial_str = spa_dict_lookup(props, PW_KEY_OBJECT_SERIAL);
        if (serial_str) serial = strtoull(serial_str, NULL, 10);

        goOnNodeGlobal(id, name, media_class ? media_class : "", serial);
        return;
    }
}

static const struct pw_core_events core_events = {
    PW_VERSION_CORE_EVENTS,
    .info = NULL,
    .done = on_core_done,
    .error = on_core_error,
};

static const struct pw_registry_events registry_events = {
    PW_VERSION_REGISTRY_EVENTS,
    .global = on_registry_global,
    .global_remove = NULL,
};

// --- FILTER EVENTS ---

static void on_process(void *userdata, struct spa_io_position *position) {
    goOnProcess(userdata, position);
}

static void on_state_changed(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, const char *error) {
    goOnStateChanged(userdata, old, state, (char *)error);
}

static const struct pw_filter_events filter_events = {
    PW_VERSION_FILTER_EVENTS,
    .state_changed = on_state_changed,
    .process = on_process,
};

// --- PORT HANDLES ---
static void *port_handles[4] = {0};

// --- INIT ---

int zen_init(void) {
    pw_init(NULL, NULL);

    pw.loop = pw_thread_loop_new("zen-dsp-thread", NULL);
    if (!pw.loop) return -1;

    if (pw_thread_loop_start(pw.loop) < 0) return -2;

    pw_thread_loop_lock(pw.loop);

    struct pw_properties *props = pw_properties_new(
        PW_KEY_CONFIG_NAME, "client-rt.conf",
        PW_KEY_MEDIA_TYPE, "Audio",
        PW_KEY_MEDIA_CATEGORY, "Manager",
        PW_KEY_MEDIA_ROLE, "Music",
        NULL
    );
    pw.context = pw_context_new(pw_thread_loop_get_loop(pw.loop), props, 0);
    pw_properties_free(props);
    if (!pw.context) { pw_thread_loop_unlock(pw.loop); return -3; }

    pw.core = pw_context_connect(pw.context, NULL, 0);
    if (!pw.core) { pw_thread_loop_unlock(pw.loop); return -4; }

    pw_core_add_listener(pw.core, &core_listener, &core_events, NULL);

    pw.registry = pw_core_get_registry(pw.core, PW_VERSION_REGISTRY, 0);
    if (!pw.registry) { pw_thread_loop_unlock(pw.loop); return -5; }

    pw_registry_add_listener(pw.registry, &registry_listener, &registry_events, NULL);

    pw_thread_loop_unlock(pw.loop);
    return 0;
}

// --- CREATE NULL SINK ---

struct pw_proxy *zen_create_sink(void) {
    struct pw_properties *props = pw_properties_new(
        "node.name", "zen-dsp-sink",
        "node.description", "ZenDSP Sink",
        "node.virtual", "true",
        "node.passive", "out",
        "factory.name", "support.null-audio-sink",
        "media.class", "Audio/Sink",
        "audio.position", "FL,FR",
        "monitor.channel-volumes", "true",
        NULL
    );

    pw_thread_loop_lock(pw.loop);
    struct pw_proxy *proxy = pw_core_create_object(pw.core, "adapter",
        PW_TYPE_INTERFACE_Node, PW_VERSION_NODE,
        &props->dict, 0);
    pw_properties_free(props);

    // sync to let the node appear in registry
    pw_core_sync(pw.core, PW_ID_CORE, 0);
    pw_thread_loop_wait(pw.loop);
    pw_thread_loop_unlock(pw.loop);
    return proxy;
}

// --- CREATE FILTER ---

struct pw_filter *zen_create_filter(void) {
    struct pw_properties *props = pw_properties_new(
        "node.name", "zen-dsp-eq",
        "node.nick", "ZenDSP EQ",
        "node.description", "ZenDSP Equalizer Filter",
        "media.type", "Audio",
        "media.category", "Filter",
        "media.role", "DSP",
        "node.passive", "true",
        NULL
    );

    pw_thread_loop_lock(pw.loop);
    struct pw_filter *f = pw_filter_new(pw.core, "zen-dsp-eq", props);
    pw_properties_free(props);
    if (!f) { pw_thread_loop_unlock(pw.loop); return NULL; }

    struct pw_properties *pin = pw_properties_new(
        PW_KEY_FORMAT_DSP, "32 bit float mono audio",
        PW_KEY_PORT_NAME, "input_FL",
        "audio.channel", "FL", NULL
    );
    port_handles[0] = pw_filter_add_port(f, PW_DIRECTION_INPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pin, NULL, 0);
    pw_properties_free(pin);

    pin = pw_properties_new(
        PW_KEY_FORMAT_DSP, "32 bit float mono audio",
        PW_KEY_PORT_NAME, "input_FR",
        "audio.channel", "FR", NULL
    );
    port_handles[1] = pw_filter_add_port(f, PW_DIRECTION_INPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pin, NULL, 0);
    pw_properties_free(pin);

    struct pw_properties *pout = pw_properties_new(
        PW_KEY_FORMAT_DSP, "32 bit float mono audio",
        PW_KEY_PORT_NAME, "output_FL",
        "audio.channel", "FL", NULL
    );
    port_handles[2] = pw_filter_add_port(f, PW_DIRECTION_OUTPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pout, NULL, 0);
    pw_properties_free(pout);

    pout = pw_properties_new(
        PW_KEY_FORMAT_DSP, "32 bit float mono audio",
        PW_KEY_PORT_NAME, "output_FR",
        "audio.channel", "FR", NULL
    );
    port_handles[3] = pw_filter_add_port(f, PW_DIRECTION_OUTPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pout, NULL, 0);
    pw_properties_free(pout);

    pw_filter_add_listener(f, &(struct spa_hook){0}, &filter_events, NULL);

    pw.filter = f;
    pw_thread_loop_unlock(pw.loop);
    return f;
}

int zen_connect_filter(struct pw_filter *f) {
    pw_thread_loop_lock(pw.loop);
    int ret = pw_filter_connect(f, PW_FILTER_FLAG_RT_PROCESS, NULL, 0);
    pw_core_sync(pw.core, PW_ID_CORE, 0);
    pw_thread_loop_wait(pw.loop);
    pw_thread_loop_unlock(pw.loop);
    return ret;
}

uint32_t zen_get_filter_node_id(void) {
    return pw_filter_get_node_id(pw.filter);
}

void *zen_get_port(int idx) { return port_handles[idx]; }

// --- LINKING (PipeWire native, like JamesDSP) ---

int zen_link_nodes_by_id(uint32_t output_node_id, uint32_t input_node_id) {
    struct pw_properties *props = pw_properties_new(
        PW_KEY_LINK_OUTPUT_NODE, NULL,
        PW_KEY_LINK_INPUT_NODE, NULL,
        PW_KEY_LINK_PASSIVE, "true",
        PW_KEY_OBJECT_LINGER, "false",
        NULL
    );

    char buf[32];
    snprintf(buf, sizeof(buf), "%u", output_node_id);
    pw_properties_set(props, PW_KEY_LINK_OUTPUT_NODE, buf);
    snprintf(buf, sizeof(buf), "%u", input_node_id);
    pw_properties_set(props, PW_KEY_LINK_INPUT_NODE, buf);

    pw_thread_loop_lock(pw.loop);
    struct pw_proxy *proxy = pw_core_create_object(pw.core, "link-factory",
        PW_TYPE_INTERFACE_Link, PW_VERSION_LINK,
        &props->dict, 0);
    pw_properties_free(props);

    int ret = 0;
    if (proxy) ret = 1;

    pw_core_sync(pw.core, PW_ID_CORE, 0);
    pw_thread_loop_wait(pw.loop);
    pw_thread_loop_unlock(pw.loop);
    return ret;
}

// --- METADATA ROUTING ---

int zen_set_stream_target(uint32_t stream_id, uint32_t target_id, uint64_t target_serial) {
    if (!pw.metadata) return -1;

    char id_str[32], serial_str[32];
    snprintf(id_str, sizeof(id_str), "%u", target_id);
    snprintf(serial_str, sizeof(serial_str), "%lu", (unsigned long)target_serial);

    pw_thread_loop_lock(pw.loop);
    // target.node for backward compat, target.object for modern
    pw_metadata_set_property(pw.metadata, stream_id, "target.node", "Spa:Id", id_str);
    pw_metadata_set_property(pw.metadata, stream_id, "target.object", "Spa:Id", serial_str);
    pw_core_sync(pw.core, PW_ID_CORE, 0);
    pw_thread_loop_wait(pw.loop);
    pw_thread_loop_unlock(pw.loop);
    return 0;
}

int zen_has_metadata(void) {
    return pw.metadata != NULL ? 1 : 0;
}
*/
import "C"
import (
	"fmt"
	"log"
	"sync"
	"time"
	"unsafe"
)

// nodeMap tracks PipeWire nodes discovered via registry
type nodeInfo struct {
	id     uint32
	serial uint64
	name   string
	class  string
}

var (
	nodesMu sync.Mutex
	nodes   []nodeInfo
)

//export goOnNodeGlobal
func goOnNodeGlobal(id C.uint32_t, name, mediaClass *C.char, serial C.uint64_t) {
	nodesMu.Lock()
	nodes = append(nodes, nodeInfo{
		id:     uint32(id),
		serial: uint64(serial),
		name:   C.GoString(name),
		class:  C.GoString(mediaClass),
	})
	nodesMu.Unlock()
}

//export goOnCoreError
func goOnCoreError(message *C.char) {
	log.Printf("[pw] core error: %s", C.GoString(message))
}

var filterNodeID uint32

func SetupFilter() error {
	ret := int(C.zen_init())
	if ret < 0 {
		return fmt.Errorf("zen_init failed: code=%d", ret)
	}
	log.Printf("[eqd] PipeWire core initialized")

	// Wait for metadata to become available
	log.Printf("[eqd] waiting for metadata...")
	waitForMetadata()

	// 1. Create null-audio-sink (like JamesDSP's jamesdsp_sink)
	sinkProxy := C.zen_create_sink()
	if sinkProxy == nil {
		return fmt.Errorf("zen_create_sink failed")
	}
	log.Printf("[eqd] null-audio-sink created: zen-dsp-sink")

	// Wait for sink node to appear in registry
	time.Sleep(500 * time.Millisecond)

	// 2. Create pw_filter (like JamesDSP's PwBasePlugin)
	filterPtr := C.zen_create_filter()
	if filterPtr == nil {
		return fmt.Errorf("zen_create_filter failed")
	}
	log.Printf("[eqd] pw_filter created")

	// 3. Connect filter
	ret = int(C.zen_connect_filter(filterPtr))
	if ret < 0 {
		return fmt.Errorf("zen_connect_filter failed: %d", ret)
	}
	log.Printf("[eqd] pw_filter connected")

	filterNodeID = uint32(C.zen_get_filter_node_id())
	log.Printf("[eqd] filter node id=%d", filterNodeID)

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
