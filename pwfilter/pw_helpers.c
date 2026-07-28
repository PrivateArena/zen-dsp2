#include "pw_helpers.h"
#include <pipewire/pipewire.h>
#include <pipewire/filter.h>
#include <pipewire/thread-loop.h>
#include <pipewire/extensions/metadata.h>
#include <spa/param/audio/format-utils.h>
#include <spa/utils/json.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// --- Go-exported forward declarations ---
extern void goOnProcess(void *userdata, struct spa_io_position *position);
extern void goOnStateChanged(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, char *error);
extern void goOnNodeGlobal(uint32_t id, char *name, char *media_class, uint64_t serial);
extern void goOnCoreError(char *message);

// --- CORE STATE (for all PipeWire operations, like JamesDSP uses one core) ---
static struct {
    struct pw_thread_loop *loop;
    struct pw_context *context;
    struct pw_core *core;
    struct pw_registry *registry;
    struct pw_metadata *metadata;
    struct pw_proxy *null_sink;
    struct pw_filter *filter;
} pw = {0};

static struct spa_hook core_listener, registry_listener, metadata_listener;
static struct spa_hook filter_listener;
static void *port_handles[4] = {0};

// --- CALLBACKS ---

static void on_core_error(void *data, uint32_t id, int seq, int res, const char *message) {
    goOnCoreError((char*)message);
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
            pw.metadata = (struct pw_metadata *)pw_registry_bind(
                pw.registry, id, type, PW_VERSION_METADATA, 0);
            if (pw.metadata) {
                struct pw_metadata_events mevents = {
                    PW_VERSION_METADATA_EVENTS,
                    .property = NULL,
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

        goOnNodeGlobal(id, (char*)name, (char*)(media_class ? media_class : ""), serial);
        return;
    }
}

static void on_process(void *userdata, struct spa_io_position *position) {
    goOnProcess(userdata, position);
}

static void on_state_changed(void *userdata, enum pw_filter_state old,
                              enum pw_filter_state state, const char *error) {
    goOnStateChanged(userdata, old, state, (char *)error);
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

static const struct pw_filter_events filter_events = {
    PW_VERSION_FILTER_EVENTS,
    .state_changed = on_state_changed,
    .process = on_process,
};

// --- INIT (like JamesDSP's PwPipelineManager) ---

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

    // Sync to process initial registry events
    pw_core_sync(pw.core, PW_ID_CORE, 0);
    pw_thread_loop_wait(pw.loop);
    pw_thread_loop_unlock(pw.loop);
    return 0;
}

// --- CREATE NULL SINK (like JamesDSP's jamesdsp_sink) ---

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

    pw_core_sync(pw.core, PW_ID_CORE, 0);
    pw_thread_loop_wait(pw.loop);
    pw_thread_loop_unlock(pw.loop);
    return proxy;
}

// --- CREATE FILTER (using pw_filter_new_simple, which connected before the rewrite) ---

struct pw_filter *zen_create_filter(void) {
    // Use pw_filter_new_simple which creates its own context/core internally
    // Pass our loop for event integration
    struct pw_loop *loop = pw_thread_loop_get_loop(pw.loop);

    struct pw_properties *props = pw_properties_new(
        "media.type", "Audio",
        "media.category", "Filter",
        "media.role", "DSP",
        "node.name", "zen-dsp-eq",
        "node.description", "ZenDSPEqualizer",
        NULL
    );

    struct pw_filter *f = pw_filter_new_simple(loop, "zen-dsp-eq",
        props, &filter_events, NULL);
    pw_properties_free(props);
    if (!f) return NULL;

    struct pw_properties *pin, *pout;

    pin = pw_properties_new("port.name", "input_FL", NULL);
    port_handles[0] = pw_filter_add_port(f, PW_DIRECTION_INPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pin, NULL, 0);
    pw_properties_free(pin);

    pin = pw_properties_new("port.name", "input_FR", NULL);
    port_handles[1] = pw_filter_add_port(f, PW_DIRECTION_INPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pin, NULL, 0);
    pw_properties_free(pin);

    pout = pw_properties_new("port.name", "output_FL", "audio.format", "F32", "audio.channels", "1", "audio.position", "FL", NULL);
    port_handles[2] = pw_filter_add_port(f, PW_DIRECTION_OUTPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pout, NULL, 0);
    pw_properties_free(pout);

    pout = pw_properties_new("port.name", "output_FR", "audio.format", "F32", "audio.channels", "1", "audio.position", "FR", NULL);
    port_handles[3] = pw_filter_add_port(f, PW_DIRECTION_OUTPUT,
        PW_FILTER_PORT_FLAG_MAP_BUFFERS, 0, pout, NULL, 0);
    pw_properties_free(pout);

    return f;
}

// Connect + add listener (JamesDSP pattern: connect first, then listener)
int zen_connect_filter(struct pw_filter *f) {
    // Use the filter's own core context
    int ret = pw_filter_connect(f, PW_FILTER_FLAG_RT_PROCESS, NULL, 0);
    return ret;
}

uint32_t zen_get_filter_node_id(void) {
    return pw_filter_get_node_id(pw.filter);
}

void *zen_get_port(int idx) {
    if (idx < 0 || idx > 3) return NULL;
    return port_handles[idx];
}

// --- LINKING (PipeWire native, like JamesDSP's link_nodes) ---

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

    int ret = (proxy != NULL) ? 1 : 0;

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
