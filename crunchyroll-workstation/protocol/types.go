package protocol

type Details struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Description    string   `json:"description,omitempty"`
	OriginalTitle  string   `json:"originalTitle,omitempty"`
	Year           int      `json:"year,omitempty"`
	Seconds        int      `json:"seconds,omitempty"`
	Genres         []string `json:"genres,omitempty"`
	Poster         string   `json:"poster,omitempty"`
	Backdrop       string   `json:"backdrop,omitempty"`
	Logo           string   `json:"logo,omitempty"`
	Thumbnail      string   `json:"thumbnail,omitempty"`
	SeriesID       string   `json:"seriesId,omitempty"`
	SeriesTitle    string   `json:"seriesTitle,omitempty"`
	SeasonID       string   `json:"seasonId,omitempty"`
	SeasonNumber   int      `json:"seasonNumber,omitempty"`
	NumberingKnown bool     `json:"numberingKnown"`
	EpisodeNumber  float64  `json:"episodeNumber,omitempty"`
	Source         string   `json:"source"`
	FetchedAt      string   `json:"fetchedAt"`
}
type Episode struct {
	ID       string   `json:"id"`
	Number   float64  `json:"number"`
	Title    string   `json:"title"`
	Seconds  int      `json:"seconds"`
	Metadata Details  `json:"metadata"`
	Audio    []string `json:"audio,omitempty"`
}
type Season struct {
	ID          string    `json:"id"`
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Episodes    []Episode `json:"episodes"`
	NumberKnown bool      `json:"numberKnown"`
}
type Series struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Seasons  []Season `json:"seasons"`
	Metadata Details  `json:"metadata"`
}
type Request struct {
	Action     string `json:"action"`
	Controlled bool   `json:"controlled"`
	ETPRT      string `json:"etpRt"`
	URL        string `json:"url"`
	ID         string `json:"id"`
	Audio      string `json:"audio"`
	Height     int    `json:"height"`
	Output     string `json:"output"`
}
type Subtitle struct{ Language, Name, Kind, Format, URL, ID string }
type Result struct {
	Event             string     `json:"event"`
	Error             string     `json:"error,omitempty"`
	Details           Details    `json:"details"`
	Series            *Series    `json:"series,omitempty"`
	Heights           []int      `json:"heights"`
	Audio             []string   `json:"audio"`
	SubtitleLanguages []string   `json:"subtitleLanguages"`
	Subtitles         []Subtitle `json:"subtitles,omitempty"`
	Output            string     `json:"output,omitempty"`
	Cookie            string     `json:"cookie,omitempty"`
	Detail            string     `json:"detail,omitempty"`
	Progress          int        `json:"progress,omitempty"`
}
