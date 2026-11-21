package sublang

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
)

var lines = map[string][]string{
	"en": {"Where are you going? I told you to wait for me at the station.", "We don't have much time left before the train leaves.",
		"I know, but I had to see my mother one last time.", "She's been ill for months and nobody tells me anything.",
		"You should have called me. I would have come with you.", "Let's go, the others are already waiting outside."},
	"fr": {"Où vas-tu ? Je t'ai dit de m'attendre à la gare.", "Il ne nous reste plus beaucoup de temps avant le départ du train.",
		"Je sais, mais je devais voir ma mère une dernière fois.", "Elle est malade depuis des mois et personne ne me dit rien.",
		"Tu aurais dû m'appeler. Je serais venu avec toi.", "Allons-y, les autres nous attendent déjà dehors."},
	"de": {"Wohin gehst du? Ich habe dir gesagt, du sollst am Bahnhof auf mich warten.", "Wir haben nicht mehr viel Zeit, bevor der Zug abfährt.",
		"Ich weiß, aber ich musste meine Mutter ein letztes Mal sehen.", "Sie ist seit Monaten krank und niemand sagt mir etwas.",
		"Du hättest mich anrufen sollen. Ich wäre mitgekommen.", "Lass uns gehen, die anderen warten schon draußen."},
}

// srt writes cues every five seconds for the whole length.
func srt(lang string, seconds int) string {
	var b strings.Builder
	for i, t := 0, 0; t+4 < seconds; i, t = i+1, t+5 {
		fmt.Fprintf(&b, "%d\n00:%02d:%02d,000 --> 00:%02d:%02d,000\n%s\n\n", i+1, t/60, t%60, (t+4)/60, (t+4)%60, lines[lang][i%len(lines[lang])])
	}
	return b.String()
}

// Subtitle tracks without a language get it from their text: embedded ones read in three
// stretches with FFmpeg, subtitle files read whole; a track whose stretches don't agree stays
// unknown; a rescan keeps what was read (PLAY-20).
func TestSubtitleLanguages(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("needs ffmpeg")
	}
	ctx := context.Background()
	dir := t.TempDir()
	const length = 600
	os.WriteFile(filepath.Join(dir, "en.srt"), []byte(srt("en", length)), 0o644)
	os.WriteFile(filepath.Join(dir, "fr.srt"), []byte(srt("fr", length)), 0o644)
	video := filepath.Join(dir, "film.mkv")
	out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", fmt.Sprintf("color=black:s=64x36:r=1:d=%d", length),
		"-i", filepath.Join(dir, "en.srt"), "-i", filepath.Join(dir, "fr.srt"), "-map", "0:v", "-map", "1", "-map", "2",
		"-c:v", "libx264", "-preset", "ultrafast", "-c:s", "srt", "-metadata:s:s:0", "language=und", "-metadata:s:s:1", "language=und", video).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v %s", err, out)
	}
	sidecar := filepath.Join(dir, "film.srt")
	os.WriteFile(sidecar, []byte("\ufeff"+srt("de", length)), 0o644)
	mixed := filepath.Join(dir, "film.mixed.srt")
	os.WriteFile(mixed, []byte(srt("en", 40)+srt("fr", 40)+"99\n00:09:00,000 --> 00:09:04,000\nOK\n"), 0o644)

	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := d.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Movies', 'movies')`)
	item := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 1, 1, ?, 1)`, v, lib, video, length*1000)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 0, 'video', 'h264')`, f)
	en := exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 1, 'subtitle', 'subrip')`, f)
	fr := exec(`INSERT INTO streams (file_id, stream_index, kind, codec, language) VALUES (?, 2, 'subtitle', 'subrip', 'und')`, f)
	de := exec(`INSERT INTO streams (file_id, kind, codec, external_path) VALUES (?, 'subtitle', 'subrip', ?)`, f, sidecar)
	mx := exec(`INSERT INTO streams (file_id, kind, codec, external_path) VALUES (?, 'subtitle', 'subrip', ?)`, f, mixed)
	pgs := exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 3, 'subtitle', 'hdmv_pgs_subtitle')`, f)

	s := &Service{DB: d, FFmpeg: ff}
	msg, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lang := func(id int64) (string, bool) {
		var l sql.NullString
		var detected bool
		d.QueryRow(`SELECT language, language_detected FROM streams WHERE id = ?`, id).Scan(&l, &detected)
		return l.String, detected
	}
	for id, want := range map[int64]string{en: "eng", fr: "fre", de: "ger"} {
		if l, det := lang(id); l != want || !det {
			t.Errorf("stream %d: %q detected=%v, want %q (%s)", id, l, det, want, msg)
		}
	}
	if l, det := lang(mx); l != "" || det {
		t.Errorf("a mixed file got %q", l)
	}
	if l, _ := lang(pgs); l != "" {
		t.Errorf("a picture track got %q", l)
	}
	// Read once: nothing to do next time.
	if msg, _ := s.Run(ctx); msg != "No subtitles to read" {
		t.Fatalf("read again: %s", msg)
	}
	// A rescan writes the streams again: the languages come back.
	exec(`DELETE FROM streams WHERE file_id = ? AND kind = 'subtitle'`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 1, 'subtitle', 'subrip')`, f)
	exec(`INSERT INTO streams (file_id, kind, codec, external_path) VALUES (?, 'subtitle', 'subrip', ?)`, f, sidecar)
	fi, _ := os.Stat(sidecar)
	if err := db.ReapplySubtitleLanguages(ctx, d, f, map[string][2]int64{sidecar: {fi.Size(), fi.ModTime().Unix()}}); err != nil {
		t.Fatal(err)
	}
	var got []string
	rows, _ := d.Query(`SELECT COALESCE(language, '') FROM streams WHERE file_id = ? AND kind = 'subtitle' ORDER BY id`, f)
	for rows.Next() {
		var l string
		rows.Scan(&l)
		got = append(got, l)
	}
	rows.Close()
	if strings.Join(got, ",") != "eng,ger" {
		t.Fatalf("after a rescan: %v", got)
	}
}

func TestDialogue(t *testing.T) {
	ass := "[Script Info]\nTitle: x\nScriptType: v4.00+\n\n[Events]\nFormat: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n" +
		"Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\\i1}Hello,\\Nthere{\\i0}\n"
	if got := Dialogue([]byte(ass)); len(got) != 1 || got[0] != "Hello, there" {
		t.Fatalf("ass: %q", got)
	}
	vtt := "WEBVTT\n\n00:00:01.000 --> 00:00:02.000\n<i>Bonjour</i> tout le monde\n"
	if got := Dialogue([]byte(vtt)); len(got) != 1 || got[0] != "Bonjour tout le monde" {
		t.Fatalf("vtt: %q", got)
	}
	if Identify([]string{"OK", "Yes", "No"}) != "" {
		t.Fatal("too little text")
	}
	ru := "Куда ты идёшь? Я же сказал тебе ждать меня на вокзале. У нас осталось совсем мало времени до отправления поезда. Я знаю, но мне нужно было увидеть маму в последний раз."
	if got := Identify([]string{ru, ru, ru}); got != "rus" {
		t.Fatalf("russian: %q", got)
	}
}
