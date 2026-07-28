package pwfilter

import (
	"log"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func SetupRouting() {
	time.Sleep(600 * time.Millisecond)

	hwSink := findDefaultSinkPactl()
	if hwSink == "" {
		log.Printf("[rt] could not find default hardware sink")
		return
	}
	log.Printf("[rt] hardware sink: %s", hwSink)

	vsinkID := createVirtualSink()
	if vsinkID < 0 {
		log.Printf("[rt] virtual sink creation failed")
		return
	}
	log.Printf("[rt] virtual sink id: %d", vsinkID)

	time.Sleep(300 * time.Millisecond)

	linkPorts("zen-dsp-vsink:monitor_FL", "zen-dsp-eq:Input_FL")
	linkPorts("zen-dsp-vsink:monitor_FR", "zen-dsp-eq:Input_FR")

	linkFilterToSink(hwSink)

	setDefaultSink(vsinkID)

	log.Printf("[rt] routing complete")
}

func findDefaultSinkPactl() string {
	out, err := exec.Command("pactl", "info").Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "Default Sink:") {
			return strings.TrimSpace(strings.TrimPrefix(l, "Default Sink:"))
		}
	}
	return ""
}

func createVirtualSink() int {
	// Method 1: pw-cli create-node with JSON
	json := `{"factory.name":"support.null-audio-sink","node.name":"zen-dsp-vsink","node.description":"Zen DSP Virtual Sink","media.class":"Audio/Sink","audio.position":["FL","FR"]}`
	out, err := exec.Command("pw-cli", "create-node", "adapter", json).CombinedOutput()
	if err != nil {
		log.Printf("[rt] create-node failed: %v\n%s", err, string(out))
	} else {
		idStr := strings.TrimSpace(string(out))
		log.Printf("[rt] create-node output: %q", idStr)
		if id, err := strconv.Atoi(idStr); err == nil {
			return id
		}
	}

	// Method 2: pactl load-module module-null-sink
	log.Printf("[rt] trying pactl load-module module-null-sink")
	out2, err := exec.Command("pactl", "load-module", "module-null-sink",
		"sink_name=zen-dsp-vsink",
		"sink_properties=node.description=Zen DSP Virtual Sink").CombinedOutput()
	if err != nil {
		log.Printf("[rt] pactl failed: %v\n%s", err, string(out2))
		return -1
	}
	idStr := strings.TrimSpace(string(out2))
	if id, err := strconv.Atoi(idStr); err == nil {
		return id
	}
	log.Printf("[rt] pactl output non-numeric: %q", idStr)
	return -1
}

func linkPorts(src, dst string) {
	for i := 0; i < 5; i++ {
		out, err := exec.Command("pw-link", src, dst).CombinedOutput()
		msg := strings.TrimSpace(string(out))
		if err != nil {
			if strings.Contains(msg, "File exists") {
				log.Printf("[rt] already linked: %s -> %s", src, dst)
				return
			}
			log.Printf("[rt] link attempt %d: %s -> %s: %s", i+1, src, dst, msg)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		log.Printf("[rt] linked: %s -> %s", src, dst)
		return
	}
	log.Printf("[rt] FAILED to link %s -> %s", src, dst)
}

func linkFilterToSink(hwSink string) {
	out, err := exec.Command("pw-link", "-i").Output()
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if strings.Contains(l, hwSink) && strings.Contains(l, "playback") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) != 2 {
				continue
			}
			port := parts[1]
			if strings.Contains(port, "FL") {
				linkPorts("zen-dsp-eq:Output_FL", l)
			} else if strings.Contains(port, "FR") {
				linkPorts("zen-dsp-eq:Output_FR", l)
			}
		}
	}
}

func setDefaultSink(id int) {
	exec.Command("wpctl", "set-default", strconv.Itoa(id)).Run()
	log.Printf("[rt] set zen-dsp-vsink (id=%d) as default sink", id)
}
