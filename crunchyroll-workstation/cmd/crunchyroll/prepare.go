package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"crunchyworkstation/internal/catalog"
	"crunchyworkstation/protocol"
)

type CatalogState struct {
	Busy   bool           `json:"busy"`
	Source string         `json:"source"`
	Error  string         `json:"error"`
	Series catalog.Series `json:"series"`
}

func (b *backend) analyze(id, source string) {
	defer b.wg.Done()
	ctx, cancel := context.WithTimeout(b.ctx, 3*time.Minute)
	defer cancel()
	b.mu.Lock()
	settings := b.settings
	b.qualityCache = nil
	b.mu.Unlock()
	r, err := b.runWorker(ctx, settings, protocol.Request{Action: "analyze", ID: id, URL: source, Audio: "ja-JP"}, nil)
	a := Analysis{ID: id, Source: source, Title: r.Details.Title, Kind: r.Details.Kind, Metadata: r.Details, Series: r.Series, Heights: r.Heights, Audio: r.Audio, SubtitleLanguages: r.SubtitleLanguages, SeriesName: r.Details.SeriesTitle, Season: r.Details.SeasonNumber, Episode: r.Details.EpisodeNumber}
	if r.Series != nil {
		a.SeriesMetadata = r.Series.Metadata
		if a.Kind == "series" {
			a.SeriesName = r.Series.Title
		}
		b.rememberCatalog(*r.Series)
	}
	if err != nil {
		a.Error = err.Error()
	}
	b.mu.Lock()
	b.analysis = a
	b.mu.Unlock()
}

type qualityRecord struct {
	Heights   []int
	Subtitles []string
	At        time.Time
}
type qualityResponse struct {
	Heights   []int    `json:"heights"`
	Subtitles []string `json:"subtitles"`
}

func (b *backend) qualities(raw json.RawMessage) (qualityResponse, error) {
	var r struct {
		URL, ID string
		Audio   []string
	}
	if json.Unmarshal(raw, &r) != nil || !validContentID(r.ID) || len(r.Audio) == 0 || len(r.Audio) > 32 {
		return qualityResponse{}, errors.New("selecciona episodio e idiomas para consultar sus calidades")
	}
	b.mu.Lock()
	a := b.analysis
	settings := b.settings
	b.mu.Unlock()
	if a.Busy || a.Source != r.URL || a.Error != "" {
		return qualityResponse{}, errors.New("analiza el enlace antes de consultar calidades")
	}
	allowed := a.ID == r.ID && a.Kind != "series"
	if a.Kind == "series" && a.Series != nil {
		for _, s := range a.Series.Seasons {
			for _, e := range s.Episodes {
				if e.ID == r.ID {
					allowed = true
				}
			}
		}
	}
	if !allowed {
		return qualityResponse{}, errors.New("el episodio no pertenece al enlace analizado")
	}
	var common []int
	subtitles := []string{}
	for i, audio := range r.Audio {
		if !containsString(a.Audio, audio) {
			return qualityResponse{}, errors.New("idioma de audio no disponible")
		}
		key := r.URL + "\x00" + r.ID + "\x00" + audio
		b.mu.Lock()
		cached, ok := b.qualityCache[key]
		b.mu.Unlock()
		if !ok || time.Since(cached.At) > 3*time.Minute {
			ctx, cancel := context.WithTimeout(b.ctx, 80*time.Second)
			result, err := b.runWorker(ctx, settings, protocol.Request{Action: "options", ID: r.ID, Audio: audio}, nil)
			cancel()
			if err != nil {
				return qualityResponse{}, err
			}
			cached = qualityRecord{result.Heights, result.SubtitleLanguages, time.Now()}
			b.mu.Lock()
			if b.qualityCache == nil {
				b.qualityCache = map[string]qualityRecord{}
			}
			b.qualityCache[key] = cached
			b.mu.Unlock()
		}
		if i == 0 {
			common = append([]int{}, cached.Heights...)
		} else {
			next := []int{}
			for _, h := range common {
				if containsInt(cached.Heights, h) {
					next = append(next, h)
				}
			}
			common = next
		}
		for _, s := range cached.Subtitles {
			if !containsString(subtitles, s) {
				subtitles = append(subtitles, s)
			}
		}
	}
	if len(common) == 0 {
		return qualityResponse{}, errors.New("los idiomas elegidos no comparten una calidad disponible en este episodio")
	}
	return qualityResponse{common, subtitles}, nil
}

func qualityKey(source, id, audio string) string {
	return strings.Join([]string{source, id, audio}, "\x00")
}
