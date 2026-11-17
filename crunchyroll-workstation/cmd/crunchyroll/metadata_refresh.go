package main

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"crunchyworkstation/protocol"
)

func (b *backend) refreshMetadata(raw json.RawMessage) (bool, error) {
	var request struct{ Key string }
	if json.Unmarshal(raw, &request) != nil {
		return false, errors.New("selecciona una descarga")
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
	if job == nil || job.State != "complete" {
		return false, errors.New("selecciona una descarga completada")
	}
	if job.SubtitleBusy {
		return false, errors.New("ya se están actualizando sus archivos")
	}
	job.SubtitleBusy = true
	data, _ := json.Marshal(job)
	settings := b.settings
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ctx, cancel := context.WithTimeout(b.ctx, 4*time.Minute)
		defer cancel()
		var copy Job
		json.Unmarshal(data, &copy)
		r, err := b.runWorker(ctx, settings, protocol.Request{Action: "metadata", ID: copy.ID, URL: "https://www.crunchyroll.com/watch/" + copy.ID, Audio: copy.Audio}, nil)
		if err == nil && r.Details.SeriesID == copy.Metadata.SeriesID && r.Details.SeasonID == copy.Metadata.SeasonID && r.Details.EpisodeNumber == copy.Metadata.EpisodeNumber {
			copy.Metadata = r.Details
			if r.Series != nil {
				copy.SeriesMetadata = r.Series.Metadata
			}
		}
		copy.ExportWarnings = nil
		if exportErr := b.exportLibrary(ctx, &copy, settings); exportErr != nil {
			copy.ExportWarnings = append(copy.ExportWarnings, exportErr.Error())
		}
		copy.Detail = "Ficha MediaCenter actualizada"
		if len(copy.ExportWarnings) > 0 {
			copy.Detail = "Archivos conservados · Revisa la exportación: " + copy.ExportWarnings[0]
		}
		if err != nil {
			copy.Detail += " · No se pudo actualizar la fuente: " + err.Error()
		}
		copy.SubtitleBusy = false
		b.mu.Lock()
		*job = copy
		b.persist()
		b.mu.Unlock()
	}()
	return true, nil
}
