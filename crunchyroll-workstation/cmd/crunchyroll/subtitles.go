package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"crunchyworkstation/internal/captions"
	"crunchyworkstation/protocol"
)

func (b *backend) fetchSubtitles(raw json.RawMessage) (bool, error) {
	var request struct{ Key string }
	if json.Unmarshal(raw, &request) != nil {
		return false, errors.New("selecciona una descarga completada")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var job *Job
	for _, j := range b.jobs {
		if j.Key == request.Key {
			job = j
			break
		}
	}
	if job == nil || job.State != "complete" || !regularFile(job.Path) {
		return false, errors.New("selecciona una descarga cuyo vídeo siga en su carpeta")
	}
	if job.SubtitleBusy {
		return false, errors.New("ya se están actualizando sus archivos")
	}
	job.SubtitleBusy = true
	job.Detail = "Buscando los subtítulos elegidos…"
	data, _ := json.Marshal(job)
	settings := b.settings
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ctx, cancel := context.WithTimeout(b.ctx, 12*time.Minute)
		defer cancel()
		var copy Job
		json.Unmarshal(data, &copy)
		r, err := b.runWorker(ctx, settings, protocol.Request{Action: "subtitles", ID: copy.ID, Audio: copy.Audio}, nil)
		if err == nil {
			anchor := copy.SubtitleAnchor
			if anchor == "" {
				anchor = filepath.Join(filepath.Dir(copy.ManifestPath), "Subtitulos", filepath.Base(copy.Path))
			}
			result := captions.SaveAll(ctx, captions.Select(captionTracks(r.Subtitles), copy.SubtitleMode, copy.SubtitleLanguages), anchor, nil)
			for _, video := range copy.VideoFiles {
				addSubtitles(&copy, result, video.Path)
			}
			copy.SubtitleFailed = result.Failed
			copy.SubtitleTotal = len(copy.Subtitles) + result.Failed
			copy.Detail = fmt.Sprintf("Subtítulos guardados: %d archivos", len(copy.Subtitles))
			if exportErr := b.exportLibrary(ctx, &copy, settings); exportErr != nil {
				copy.ExportWarnings = append(copy.ExportWarnings, exportErr.Error())
				copy.Detail += " · Revisa la ficha"
			}
		} else {
			copy.Detail = "No se pudieron actualizar los subtítulos: " + err.Error()
		}
		copy.SubtitleBusy = false
		b.mu.Lock()
		*job = copy
		b.persist()
		b.mu.Unlock()
	}()
	return true, nil
}
