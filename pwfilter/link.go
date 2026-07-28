package pwfilter

import (
	"log"
	"os/exec"
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
	if vsinkID == "" {
		log.Printf("[rt] virtual sink creation failed")
		return
	}
	log.Printf("[rt] virtual sink id: %s", vsinkID)

	time.Sleep(300 * time.Millisecond)

	linkPorts("zen-dsp-vsink:monitor_FL", "zen-dsp-eq:Input_FL")
	linkPorts("zen-dsp-vsink:monitor_FR", "zen-dsp-eq:Input_FR")

	linkFilterToSink(hwSink)

	setDefaultSink(vsinkID)

	log.Printf("[rt] routing complete — audio flows: apps → vsink → filter → hw")
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

func createVirtualSink() string {
	cmd := exec.Command("pw-cli", "create-node", "adapter", `{
		"factory.name":"support.null-audio-sink",
		"node.name":"zen-dsp-vsink",
		"node.description":"Zen DSP Virtual Sink",
		"media.class":"Audio/Sink",
		"audio.position":["FL","FR"],
		"monitor.channel-volumes":"true"
	}`)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[rt] create vsink failed: %v\n%s", err, string(out))
		return ""
	}
	id := strings.TrimSpace(string(out))
	log.Printf("[rt] created vsink id=%s", id)
	return id
}

func linkPorts(src, dst string) {
	for i := 0; i < 5; i++ {
		out, err := exec.Command("pw-link", src, dst).CombinedOutput()
		msg := strings.TrimSpace(string(out))
		if err != nil {
			if strings.Contains(msg, "File exists") {
				log.Printf("[rt] already linked: %s → %s", src, dst)
				return
			}
			log.Printf("[rt] link attempt %d: %s → %s: %s", i+1, src, dst, msg)
			time.Sleep(300 * time.Millisecond)
			continue
		}
		log.Printf("[rt] linked: %s → %s", src, dst)
		return
	}
	log.Printf("[rt] FAILED to link %s → %s", src, dst)
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

func setDefaultSink(id string) {
	exec.Command("wpctl", "set-default", id).Run()
	log.Printf("[rt] set zen-dsp-vsink (id=%s) as default sink", id)
}
