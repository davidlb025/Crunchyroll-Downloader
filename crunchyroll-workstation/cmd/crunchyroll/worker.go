package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"crunchyworkstation/internal/captions"
	"crunchyworkstation/internal/provider"
	"crunchyworkstation/protocol"
)

func (b *backend) runWorker(ctx context.Context, settings Settings, request protocol.Request, progress func(protocol.Result)) (protocol.Result, error) {
	if request.Action != "login" {
		cookies, err := provider.ReadSession(b.sessionPath())
		if err != nil {
			return protocol.Result{}, err
		}
		request.ETPRT = cookies["etp_rt"]
	}
	request.Controlled = true
	cmd := exec.Command(filepath.Join(b.base, "assets", "crunchyroll-engine.exe"))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Dir = b.base
	if settings.CDM != "" {
		if info, err := os.Stat(settings.CDM); err == nil {
			if info.IsDir() {
				cmd.Dir = settings.CDM
			} else {
				cmd.Dir = filepath.Dir(settings.CDM)
			}
		}
	}
	if ffmpeg, err := exec.LookPath(settings.FFmpeg); err == nil {
		for _, v := range os.Environ() {
			if !strings.HasPrefix(strings.ToUpper(v), "PATH=") {
				cmd.Env = append(cmd.Env, v)
			}
		}
		cmd.Env = append(cmd.Env, "PATH="+filepath.Dir(ffmpeg)+";"+os.Getenv("PATH"))
	}
	input, err := cmd.StdinPipe()
	if err != nil {
		return protocol.Result{}, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return protocol.Result{}, err
	}
	cmd.Stderr = io.Discard
	if err = cmd.Start(); err != nil {
		input.Close()
		return protocol.Result{}, errors.New("no se pudo iniciar assets/crunchyroll-engine.exe")
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
			input.Close()
			select {
			case <-finished:
			case <-time.After(12 * time.Second):
				cmd.Process.Kill()
			}
		case <-finished:
		}
	}()
	if err = json.NewEncoder(input).Encode(request); err != nil {
		input.Close()
		cmd.Wait()
		return protocol.Result{}, err
	}
	var result protocol.Result
	received := false
	scanner := bufio.NewScanner(output)
	scanner.Buffer(make([]byte, 65536), 12<<20)
	for scanner.Scan() {
		var event protocol.Result
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			continue
		}
		switch event.Event {
		case "progress":
			if progress != nil {
				progress(event)
			}
		case "result":
			result = event
			received = true
		}
	}
	err = cmd.Wait()
	input.Close()
	if ctx.Err() != nil {
		return protocol.Result{}, ctx.Err()
	}
	if scanner.Err() != nil {
		return protocol.Result{}, errors.New("la respuesta del motor fue incompleta")
	}
	if !received || err != nil {
		return protocol.Result{}, errors.New("el motor se cerró antes de completar la operación")
	}
	if result.Error != "" {
		return result, errors.New(result.Error)
	}
	return result, nil
}
func captionTracks(tracks []protocol.Subtitle) []captions.Track {
	result := []captions.Track{}
	for _, t := range tracks {
		result = append(result, captions.Track{Language: t.Language, Kind: t.Kind, Format: t.Format, URL: t.URL, ID: t.ID, Name: t.Name})
	}
	return result
}
func (b *backend) downloadVariant(ctx context.Context, j *Job, settings Settings, audio string, height int, status func(string, string, int)) error {
	stage, err := os.MkdirTemp(b.home, "transfer-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	status("downloading", "Consultando las pistas y la calidad del idioma elegido…", 1)
	result, err := b.runWorker(ctx, settings, protocol.Request{Action: "download", ID: j.ID, Audio: audio, Height: height, Output: stage}, func(event protocol.Result) { status("downloading", event.Detail, event.Progress) })
	if err != nil {
		return err
	}
	if !regularFile(result.Output) || !withinRoot(stage, result.Output) {
		return errors.New("el motor devolvió una ruta multimedia no válida")
	}
	// Normalize the verified container for the common library without changing
	// resolution or re-encoding video/audio. The service engine remains intact.
	staged := filepath.Join(stage, "bundle.mp4")
	if err = runMediaCopy(ctx, settings.FFmpeg, "-i", result.Output, "-map", "0:v:0", "-map", "0:a:0", "-c:v", "copy", "-c:a", "copy", "-movflags", "+faststart", staged); err != nil {
		return fmt.Errorf("no se pudo preparar el contenedor: %w", err)
	}
	return b.finalizeVariant(ctx, j, settings, staged, height, audio, captions.Select(captionTracks(result.Subtitles), j.SubtitleMode, j.SubtitleLanguages), status)
}

func (b *backend) importLegacySession() {
	if regularFile(b.sessionPath()) {
		return
	}
	home, _ := os.UserHomeDir()
	path := filepath.Join(home, ".crunchyroll.config", "config.json")
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 4<<20 {
		return
	}
	var old struct {
		Cookie string `json:"etp_rt"`
		Output string `json:"output_dir"`
	}
	if json.Unmarshal(data, &old) != nil || old.Cookie == "" {
		return
	}
	if provider.SaveSession(b.sessionPath(), map[string]string{"etp_rt": old.Cookie}) == nil {

		atomicJSON(filepath.Join(b.home, "settings.json"), b.settings)
	}
}

func (b *backend) login() (bool, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.loginBusy {
		return false, errors.New("ya hay un acceso al navegador en curso")
	}
	b.loginBusy = true
	b.loginError = ""
	settings := b.settings
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		ctx, cancel := context.WithTimeout(b.ctx, 10*time.Minute)
		defer cancel()
		result, err := b.runWorker(ctx, settings, protocol.Request{Action: "login"}, nil)
		if err == nil {
			err = provider.SaveSession(b.sessionPath(), map[string]string{"etp_rt": result.Cookie})
		}
		b.mu.Lock()
		defer b.mu.Unlock()
		b.loginBusy = false
		if err != nil {
			b.loginError = err.Error()
		} else {
			b.qualityCache = nil
		}
	}()
	return true, nil
}
