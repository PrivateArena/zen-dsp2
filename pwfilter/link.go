package pwfilter

import (
	"log"
	"os/exec"
	"strings"
	"time"
)

func SetupRouting() {
	time.Sleep(600 * time.Millisecond)

	unloadAllVirtualSinks()
	time.Sleep(200 * time.Millisecond)

	if !createVirtualSinkPactl() {
		log.Printf("[rt] virtual sink creation failed")
		return
	}
	log.Printf("[rt] virtual sink 'zen-dsp-vsink' created")

	time.Sleep(300 * time.Millisecond)

	linkPorts("zen-dsp-vsink:monitor_FL", "zen-dsp-eq:Input_FL")
	linkPorts("zen-dsp-vsink:monitor_FR", "zen-dsp-eq:Input_FR")

	setDefaultSink()
	time.Sleep(200 * time.Millisecond)

	outputSink := findDefaultSinkName()
	if outputSink == "" || strings.Contains(outputSink, "zen-dsp") {
		log.Printf("[rt] default sink is vsink itself, scanning for hw sink")
		outputSink = findHardwareSink()
	}
	log.Printf("[rt] output sink: %s", outputSink)

	forceResumeSink(outputSink)
	time.Sleep(300 * time.Millisecond)

	linkFilterToSink(outputSink)

	log.Printf("[rt] routing complete: apps -> vsink -> filter -> %s", outputSink)
}

func findHardwareSink() string {
	raw, err := exec.Command("pactl", "list", "sinks").CombinedOutput()
	if err != nil {
		log.Printf("[rt] pactl list sinks failed: %v\n%s", err, string(raw))
		return ""
	}

	var best string
	var fallback string
	blocks := strings.Split(string(raw), "\n\n")
	for _, block := range blocks {
		if !strings.Contains(block, "Name:") {
			continue
		}
		lines := strings.Split(block, "\n")

		var name, deviceAPI string
		isVirtual := false
		for _, l := range lines {
			l = strings.TrimSpace(l)
			if strings.HasPrefix(l, "Name:") {
				name = strings.TrimSpace(l[5:])
			}
			if strings.HasPrefix(l, "device.api") {
				parts := strings.SplitN(l, "=", 2)
				if len(parts) == 2 {
					deviceAPI = strings.Trim(strings.TrimSpace(parts[1]), "\"")
				}
			}
			if strings.HasPrefix(l, "node.virtual") && strings.Contains(l, "true") {
				isVirtual = true
			}
		}
		if name == "" || isVirtual {
			continue
		}
		if fallback == "" {
			fallback = name
		}
		if deviceAPI == "alsa" && best == "" {
			best = name
		}
	}
	if best != "" {
		log.Printf("[rt] using ALSA sink: %s", best)
		return best
	}
	if fallback != "" {
		log.Printf("[rt] using first non-virtual sink: %s", fallback)
		return fallback
	}
	return ""
}

func unloadAllVirtualSinks() {
	out, err := exec.Command("pactl", "list", "modules").CombinedOutput()
	if err != nil {
		log.Printf("[rt] pactl list modules failed: %v", err)
		return
	}
	count := 0
	for _, l := range strings.Split(string(out), "\n") {
		if strings.Contains(l, "module-null-sink") && strings.Contains(l, "zen-dsp-vsink") {
			parts := strings.SplitN(l, ":", 2)
			if len(parts) == 2 {
				moduleID := strings.TrimSpace(parts[0])
				log.Printf("[rt] unloading vsink module %s", moduleID)
				exec.Command("pactl", "unload-module", moduleID).Run()
				count++
			}
		}
	}
	if count > 0 {
		log.Printf("[rt] unloaded %d stale vsink(s)", count)
	}
}

func forceResumeSink(name string) {
	log.Printf("[rt] force-resuming sink: %s", name)
	out, err := exec.Command("pactl", "suspend-sink", name, "0").CombinedOutput()
	if err != nil {
		log.Printf("[rt] suspend-sink resume failed: %v\n%s", err, string(out))
	}
}

func findDefaultSinkName() string {
	out, err := exec.Command("pactl", "info").CombinedOutput()
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

func createVirtualSinkPactl() bool {
	out, err := exec.Command("pactl", "load-module", "module-null-sink",
		"sink_name=zen-dsp-vsink",
		"sink_properties=node.description=Zen DSP Virtual Sink").CombinedOutput()
	if err != nil {
		log.Printf("[rt] pactl load-module failed: %v\n%s", err, string(out))
		return false
	}
	log.Printf("[rt] pactl load-module output: %q", strings.TrimSpace(string(out)))
	return true
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

func setDefaultSink() {
	exec.Command("pactl", "set-default-sink", "zen-dsp-vsink").Run()
	log.Printf("[rt] default sink set to 'zen-dsp-vsink'")
}
