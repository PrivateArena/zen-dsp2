#ifndef PW_HELPERS_H
#define PW_HELPERS_H

#include <pipewire/pipewire.h>
#include <pipewire/filter.h>
#include <pipewire/thread-loop.h>
#include <pipewire/extensions/metadata.h>
#include <spa/param/audio/format-utils.h>
#include <spa/utils/json.h>

int zen_init(void);
struct pw_proxy *zen_create_sink(void);
struct pw_filter *zen_create_filter(void);
int zen_connect_filter(struct pw_filter *f);
uint32_t zen_get_filter_node_id(void);
void *zen_get_port(int idx);
int zen_link_nodes_by_id(uint32_t output_node_id, uint32_t input_node_id);
int zen_set_stream_target(uint32_t stream_id, uint32_t target_id, uint64_t target_serial);
int zen_has_metadata(void);

#endif
