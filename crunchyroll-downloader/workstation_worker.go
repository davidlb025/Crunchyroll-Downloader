//go:build workstationworker

package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"crunchyworkstation/protocol"
)

var workstationOutput sync.Mutex

func workerEmit(value protocol.Result) {
	workstationOutput.Lock()
	defer workstationOutput.Unlock()
	json.NewEncoder(os.Stdout).Encode(value)
}

// The worker reuses the original service engine. It does not alter its DRM,
// authentication, key handling or licensing behavior. Credentials arrive through
// stdin, never through process arguments or a plaintext settings file.
func main() {
	reader := bufio.NewReader(io.LimitReader(os.Stdin, 2<<20))
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return
	}
	var request protocol.Request
	if json.Unmarshal(line, &request) != nil {
		workerEmit(protocol.Result{Event: "result", Error: "petición de motor no válida"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Minute)
	defer cancel()
	if request.Controlled {
		go func() { io.Copy(io.Discard, reader); cancel() }()
	}
	result := workerRun(ctx, request)
	result.Event = "result"
	result.Error = workerSafeError(result.Error, request.ETPRT)
	workerEmit(result)
}
func workerSafeError(message, secret string) string {
	if secret != "" {
		message = strings.ReplaceAll(message, secret, "[sesión]")
	}
	if len(message) > 1200 {
		message = message[:1200]
	}
	return message
}
func workerRun(ctx context.Context, r protocol.Request) (result protocol.Result) {
	defer func() {
		if v := recover(); v != nil {
			result.Error = fmt.Sprintf("el motor no pudo completar la consulta: %v", v)
		}
	}()
	if r.Action == "login" {
		cookie, err := loginWithBrowser(ctx)
		if err != nil {
			result.Error = err.Error()
		} else {
			result.Cookie = cookie
		}
		return
	}
	if !contentIDPattern.MatchString(r.ID) {
		result.Error = "ID de Crunchyroll no válido"
		return
	}
	*etpRt = r.ETPRT
	if err := RefreshAccessToken(r.ETPRT); err != nil {
		result.Error = err.Error()
		return
	}
	switch r.Action {
	case "analyze", "metadata":
		return workerAnalyze(ctx, r)
	case "options", "subtitles":
		info := getEpisodeInfo(r.ID, r.Audio)
		id, _, ok := resolveWatchEpisodeID(r.ID, info, r.Audio, false)
		if !ok {
			result.Error = "este episodio no ofrece el idioma de audio elegido"
			return
		}
		return workerOptions(ctx, id)
	case "download":
		if !filepath.IsAbs(r.Output) || r.Height <= 0 {
			result.Error = "salida o calidad no válida"
			return
		}
		info := getEpisodeInfo(r.ID, r.Audio)
		id, locale, ok := resolveWatchEpisodeID(r.ID, info, r.Audio, false)
		if !ok {
			result.Error = "este episodio no ofrece el idioma de audio elegido"
			return
		}
		options := workerOptions(ctx, id)
		if options.Error != "" {
			return options
		}
		available := false
		for _, height := range options.Heights {
			if height == r.Height {
				available = true
			}
		}
		if !available {
			result.Error = "la calidad elegida no existe en la pista de este episodio e idioma; vuelve a analizar el enlace"
			return
		}
		info.EpisodeMetadata.AudioLocale = locale
		if err := os.MkdirAll(r.Output, 0700); err != nil {
			result.Error = err.Error()
			return
		}
		log := &workerProgress{secret: r.ETPRT}
		env := jobEnv{ctx: ctx, log: log, outputDir: r.Output, videoQuality: strconv.Itoa(r.Height) + "p", audioQuality: "192k", audioLang: r.Audio, subtitlesLang: "", dubFallback: false}
		if !downloadEpisode(env, id, info) {
			if ctx.Err() != nil {
				result.Error = "descarga cancelada"
			} else {
				result.Error = log.lastError
				if result.Error == "" {
					result.Error = "el motor no pudo completar la descarga"
				}
			}
			return
		}
		path := episodeOutputFile(info, env.videoQuality, env.dir())
		if !episodeOutputComplete(path) {
			result.Error = "el motor no produjo un archivo completo"
			return
		}
		result.Output = path
		result.Subtitles = options.Subtitles
		result.Heights = options.Heights
		return
	default:
		result.Error = "operación de motor desconocida"
		return
	}
}

type workerProgress struct{ secret, lastError, pending string }

func (w *workerProgress) Write(data []byte) (int, error) {
	w.pending += string(data)
	for {
		at := strings.IndexByte(w.pending, '\n')
		if at < 0 {
			break
		}
		line := strings.TrimSpace(w.pending[:at])
		w.pending = w.pending[at+1:]
		if strings.HasPrefix(line, "!") {
			w.lastError = workerSafeError(line, w.secret)
		}
		// Progress text is deliberately bounded; URLs, session values and
		// licensing diagnostics are not forwarded to the product interface.
		lower := strings.ToLower(line)
		switch {
		case strings.Contains(lower, "downloading:"):
			workerEmit(protocol.Result{Event: "progress", Detail: "Descargando las pistas del episodio…", Progress: 10})
		case strings.Contains(lower, "download finished"):
			workerEmit(protocol.Result{Event: "progress", Detail: "Contenedor multimedia terminado…", Progress: 88})
		case strings.Contains(lower, "segments") || strings.Contains(lower, "downloaded"):
			workerEmit(protocol.Result{Event: "progress", Detail: "Recibiendo segmentos de vídeo y audio…", Progress: 45})
		}
	}
	if len(w.pending) > 4096 {
		w.pending = ""
	}
	return len(data), nil
}

func workerOptions(ctx context.Context, id string) (result protocol.Result) {
	if err := ctx.Err(); err != nil {
		result.Error = err.Error()
		return
	}
	playbackSlot.Lock()
	defer playbackSlot.Unlock()
	episode, err := getEpisode(id)
	if err != nil {
		result.Error = err.Error()
		return
	}
	if episode.Token != "" {
		defer deleteStream(id, episode.Token)
	}
	env := defaultJobEnv()
	env.ctx = ctx
	env.log = io.Discard
	manifest, raw, err := parseManifest(env, episode.ManifestURL)
	if err != nil {
		result.Error = err.Error()
		return
	}
	var qualities []string
	if isOnDemand(manifest) {
		sets, err := parseOnDemand(raw)
		if err != nil {
			result.Error = "manifiesto de pistas no válido"
			return
		}
		qualities = qualitiesFromOnDemand(sets, true)
	} else {
		video, _ := findAdaptationSets(manifest)
		qualities = videoQualitiesFromSet(video)
	}
	for _, q := range qualities {
		height, err := strconv.Atoi(strings.TrimSuffix(q, "p"))
		if err == nil && height > 0 {
			result.Heights = append(result.Heights, height)
		}
	}
	sort.Ints(result.Heights)
	if len(result.Heights) == 0 {
		result.Error = "Crunchyroll no devuelve pistas de vídeo utilizables para este episodio"
		return
	}
	for locale, sub := range episode.Subtitles {
		if sub == nil || sub.URL == "" {
			continue
		}
		if sub.Language != "" {
			locale = sub.Language
		}
		sum := sha256.Sum256([]byte(locale + "/" + sub.URL))
		result.Subtitles = append(result.Subtitles, protocol.Subtitle{Language: locale, Name: languageNames[locale], Kind: "full", Format: "ass", URL: sub.URL, ID: hex.EncodeToString(sum[:4])})
		result.SubtitleLanguages = append(result.SubtitleLanguages, locale)
	}
	result.SubtitleLanguages = uniqueSorted(result.SubtitleLanguages)
	return
}

func workerCMS(ctx context.Context, path string) ([]map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.crunchyroll.com/content/v2/cms/"+path+"?locale=es-ES&preferred_audio_language=ja-JP", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+currentToken())
	req.Header.Set("User-Agent", userAgent)
	response, err := DoRequest(req)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return nil, fmt.Errorf("Crunchyroll no devolvió la ficha: HTTP %d", response.StatusCode)
	}
	var data struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 12<<20)).Decode(&data); err != nil {
		return nil, errors.New("ficha de Crunchyroll no válida")
	}
	return data.Data, nil
}
func workerString(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func workerMap(v any) map[string]any                 { m, _ := v.(map[string]any); return m }
func workerInt(m map[string]any, k string) int       { v, _ := m[k].(float64); return int(v) }
func workerFloat(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }
func workerImage(v any) string {
	best := ""
	area := 0.0
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if source, ok := x["source"].(string); ok {
				w, _ := x["width"].(float64)
				h, _ := x["height"].(float64)
				if best == "" || w*h > area {
					best = source
					area = w * h
				}
			}
			for _, c := range x {
				visit(c)
			}
		case []any:
			for _, c := range x {
				visit(c)
			}
		}
	}
	visit(v)
	return best
}
func workerDetails(raw map[string]any, kind string) protocol.Details {
	d := protocol.Details{ID: workerString(raw, "id"), Kind: kind, Title: workerString(raw, "title"), Description: workerString(raw, "description"), FetchedAt: time.Now().UTC().Format(time.RFC3339)}
	images := workerMap(raw["images"])
	d.Poster = workerImage(images["poster_tall"])
	d.Backdrop = workerImage(images["poster_wide"])
	d.Thumbnail = workerImage(images["thumbnail"])
	meta := workerMap(raw["episode_metadata"])
	if meta == nil {
		meta = raw
	}
	if kind == "episode" {
		d.SeriesID = workerString(meta, "series_id")
		d.SeriesTitle = workerString(meta, "series_title")
		d.SeasonID = workerString(meta, "season_id")
		d.SeasonNumber = workerInt(meta, "season_number")
		d.EpisodeNumber = workerFloat(meta, "episode_number")
		_, seasonKnown := meta["season_number"]
		_, episodeKnown := meta["episode_number"]
		d.NumberingKnown = seasonKnown && episodeKnown
		d.Seconds = workerInt(meta, "duration_ms") / 1000
		d.Source = "https://www.crunchyroll.com/watch/" + d.ID
	} else {
		d.Source = "https://www.crunchyroll.com/series/" + d.ID
	}
	return d
}
func workerAnalyze(ctx context.Context, r protocol.Request) (result protocol.Result) {
	seriesLink := strings.Contains(r.URL, "/series/")
	if !seriesLink {
		objects, err := workerCMS(ctx, "objects/"+r.ID)
		if err != nil || len(objects) == 0 {
			result.Error = "no se pudo consultar la ficha del episodio"
			if err != nil {
				result.Error = err.Error()
			}
			return
		}
		result.Details = workerDetails(objects[0], "episode")
		encoded, _ := json.Marshal(objects[0])
		var info EpisodeInfo
		json.Unmarshal(encoded, &info)
		result.Audio = audioLocalesFromEpisode(info)
		if result.Details.SeriesID != "" {
			if raw, err := workerCMS(ctx, "series/"+result.Details.SeriesID); err == nil && len(raw) > 0 {
				s := workerDetails(raw[0], "series")
				result.Series = &protocol.Series{ID: s.ID, Title: s.Title, Metadata: s}
				result.Details.Poster = s.Poster
				result.Details.Backdrop = s.Backdrop
			}
		}
		if r.Action == "metadata" {
			return
		}
		choice := pickPreferred(result.Audio, r.Audio)
		if choice == "" {
			result.Error = "el episodio no ofrece idiomas de audio"
			return
		}
		id, _, ok := resolveWatchEpisodeID(r.ID, info, choice, false)
		if !ok {
			result.Error = "no se pudo resolver el idioma de referencia"
			return
		}
		opts := workerOptions(ctx, id)
		result.Heights = opts.Heights
		result.SubtitleLanguages = opts.SubtitleLanguages
		result.Error = opts.Error
		return
	}
	seasonsRaw, err := workerCMS(ctx, "series/"+r.ID+"/seasons")
	if err != nil {
		result.Error = err.Error()
		return
	}
	s := protocol.Series{ID: r.ID, Seasons: []protocol.Season{}}
	if raw, err := workerCMS(ctx, "series/"+r.ID); err == nil && len(raw) > 0 {
		s.Metadata = workerDetails(raw[0], "series")
		s.Title = s.Metadata.Title
	}
	chosen := map[int]map[string]any{}
	for _, raw := range seasonsRaw {
		number := workerInt(raw, "season_number")
		old := chosen[number]
		if old == nil || workerString(raw, "audio_locale") == "ja-JP" {
			chosen[number] = raw
		}
	}
	numbers := []int{}
	for n := range chosen {
		numbers = append(numbers, n)
	}
	sort.Ints(numbers)
	for _, number := range numbers {
		if err := ctx.Err(); err != nil {
			result.Error = err.Error()
			return
		}
		raw := chosen[number]
		seasonID := workerString(raw, "id")
		if !contentIDPattern.MatchString(seasonID) {
			continue
		}
		episodes, err := workerCMS(ctx, "seasons/"+seasonID+"/episodes")
		if err != nil {
			result.Error = err.Error()
			return
		}
		season := protocol.Season{ID: seasonID, Number: number, Title: fmt.Sprintf("Temporada %d", number), NumberKnown: true, Episodes: []protocol.Episode{}}
		for _, raw := range episodes {
			d := workerDetails(raw, "episode")
			d.SeriesID = r.ID
			d.SeasonID = seasonID
			d.SeasonNumber = number
			d.NumberingKnown = d.EpisodeNumber > 0
			d.Poster = s.Metadata.Poster
			d.Backdrop = s.Metadata.Backdrop
			encoded, _ := json.Marshal(raw)
			var episode SeasonEpisode
			json.Unmarshal(encoded, &episode)
			audio := audioLocalesFromEpisodes([]SeasonEpisode{episode})
			result.Audio = append(result.Audio, audio...)
			if !contentIDPattern.MatchString(d.ID) {
				continue
			}
			if s.Title == "" {
				s.Title = d.SeriesTitle
			}
			season.Episodes = append(season.Episodes, protocol.Episode{ID: d.ID, Number: d.EpisodeNumber, Title: d.Title, Seconds: d.Seconds, Metadata: d, Audio: audio})
		}
		if len(season.Episodes) > 0 {
			s.Seasons = append(s.Seasons, season)
		}
	}
	if len(s.Seasons) == 0 {
		result.Error = "esta serie no devuelve episodios disponibles"
		return
	}
	s.Metadata.ID = r.ID
	s.Metadata.Title = s.Title
	s.Metadata.Kind = "series"
	result.Series = &s
	result.Details = s.Metadata
	result.Audio = uniqueSorted(result.Audio)
	e := s.Seasons[0].Episodes[0]
	choice := pickPreferred(e.Audio, r.Audio)
	if choice == "" {
		result.Error = "no se pudo confirmar el idioma del episodio de referencia"
		return
	}
	info := getEpisodeInfo(e.ID, choice)
	id, _, ok := resolveWatchEpisodeID(e.ID, info, choice, false)
	if !ok {
		result.Error = "no se pudo resolver el episodio de referencia"
		return
	}
	opts := workerOptions(ctx, id)
	result.Heights = opts.Heights
	result.SubtitleLanguages = opts.SubtitleLanguages
	result.Error = opts.Error
	return
}
