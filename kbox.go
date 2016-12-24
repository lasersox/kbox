package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/websocket"
	"html/template"
	"io/ioutil"
	"kbox"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

var (
	Version   string
	BuildTime string
)

var show = flag.String("show", "show/", "path to the karaoke show")
var song = flag.String("song", "oh_darling", "default song")
var port = flag.String("port", ":8080", "port to serve lyrics")
var templates = flag.String("templates", "resources/templates/", "templates dir")
var static = flag.String("static", "static/", "static files dir")

type KaraokeState struct {
	currentSong string
}

type Karaoke struct {
	songs    map[string]*kbox.Song
	json     *jsonpb.Marshaler
	showTmpl *template.Template
	state    *KaraokeState
	clock    *time.Ticker
	beater   Broadcaster
	count    int64
}

func (s *Karaoke) Init(
	showPath string,
	songName string,
	port string,
	templatesPath string,
	staticPath string) {

	s.json = &jsonpb.Marshaler{}
	s.state = &KaraokeState{}
	s.state.currentSong = songName

	// load songs
	s.loadSongs(showPath)
}

func (s *Karaoke) loadSongs(showPath string) {
	songPath := filepath.Join(showPath, "songs")
	s.songs = make(map[string]*kbox.Song)
	// reload songs forever
	go s.reload(songPath)
}

func (s Karaoke) reload(songPath string) {
	for {
		go s.reloadSongs(songPath)
		time.Sleep(5 * time.Second)
	}
}

func (s *Karaoke) reloadSongs(songPath string) {
	files, _ := ioutil.ReadDir(songPath)
	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".sng") {
			continue
		}
		songFile := &kbox.SongFile{}

		baseName := strings.TrimSuffix(f.Name(), ".sng")
		filePath := filepath.Join(songPath, f.Name())

		songText, e := ioutil.ReadFile(filePath)
		stopOnErr(e, "could not read song file")

		e = proto.UnmarshalText(string(songText), songFile)
		stopOnErr(e, "couldn't parse song file")

		song := songFile.Song
		urlPath := filepath.Join(songPath, baseName)
		s.songs[urlPath] = song
	}
	if len(s.songs) == 0 {
		log.Printf("No songs found in %s", songPath)
	}
}

func stopOnErr(e error, m string) {
	if e != nil {
		log.Fatalf("Err %s.\n\nSorry %s.", e, m)
	}
}

func (s *Karaoke) GetSongData(w http.ResponseWriter, r *http.Request) {
	log.Printf("access: %s", r.URL.Path)
	song := s.songs[r.URL.Path[1:]]
	if song == nil {
		http.NotFound(w, r)
		return
	}
	e := s.json.Marshal(w, song)
	stopRequestOnErr(w, e, "failed to get song data")
}

func stopRequestOnErr(w http.ResponseWriter, e error, m string) bool {
	if e != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Err %s. Sorry %s.", e, m)
	}
	return e != nil
}

func (s *Karaoke) GetKaraokeData(w http.ResponseWriter, r *http.Request) {
	js, e := json.Marshal(s.state)
	if stopRequestOnErr(w, e, "failed to execute template") {
		return
	}
	w.Write(js)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:    1024,
	WriteBufferSize:   1024,
	EnableCompression: true,
	Subprotocols:      []string{"heartbeat"},
}

type Beat struct {
	Ts    int64
	Count int64
}

func (s *Karaoke) openHeartbeatSocket(w http.ResponseWriter, r *http.Request) {
	c, e := upgrader.Upgrade(w, r, nil)
	defer c.Close()
	if stopRequestOnErr(w, e, "failed websocket handshake") {
		return
	}
	beater := s.beater.Listen()
	for b := beater.Read(); b != nil; b = beater.Read() {
		js, e := json.Marshal(b)
		if stopRequestOnErr(w, e, "json encoding fail") {
			return
		}
		e = c.WriteMessage(websocket.TextMessage, js)
		if stopRequestOnErr(w, e, "websocket failed (>ლ)") {
			return
		}
	}
}

type broadcast struct {
	c chan broadcast
	v interface{}
}

// Broadcaster allows
type Broadcaster struct {
	cc    chan broadcast
	sendc chan<- interface{}
}

// Receiver can be used to wait for a broadcast value.
type Receiver struct {
	c chan broadcast
}

// NewBroadcaster returns a new broadcaster object.
func NewBroadcaster() Broadcaster {
	cc := make(chan broadcast, 1)
	sendc := make(chan interface{})
	b := Broadcaster{
		sendc: sendc,
		cc:    cc,
	}

	go func() {
		for {
			select {
			case v := <-sendc:
				if v == nil {
					b.cc <- broadcast{}
					return
				}
				c := make(chan broadcast, 1)
				newb := broadcast{c: c, v: v}
				b.cc <- newb
				b.cc = c
			}
		}
	}()

	return b
}

// Listen starts returns a Receiver that
// listens to all broadcast values.
func (b Broadcaster) Listen() Receiver {
	return Receiver{b.cc}
}

// Write broadcasts a a value to all listeners.
func (b Broadcaster) Write(v interface{}) {
	select {
	case b.sendc <- v:
	default:
		fmt.Printf("dropping beat")
	}
}

// Read reads a value that has been broadcast,
// waiting until one is available if necessary.
func (r *Receiver) Read() interface{} {
	b := <-r.c
	v := b.v
	r.c <- b
	r.c = b.c
	return v
}

func (s *Karaoke) beat() {
	log.Print("beating...")
	s.count = 0
	var lastbeat *Beat
	for {
		t := <-s.clock.C
		s.count += 1
		beat := &Beat{Ts: t.UnixNano(), Count: s.count}
		s.beater.Write(*beat)
		if lastbeat == nil {
			lastbeat = beat
		}
		log.Print("delta: ", time.Duration(beat.Ts-lastbeat.Ts))
		lastbeat = beat
	}
}

func (s *Karaoke) Start() {
	s.clock = time.NewTicker(5 * time.Second)
	b := <-s.clock.C

	log.Printf("clock is running: %s", b)
	defer s.clock.Stop()
	s.beater = NewBroadcaster()

	go s.beat()

	fs := http.FileServer(http.Dir(*static))
	http.Handle("/", fs)
	http.HandleFunc("/show/songs/", s.GetSongData) // set router
	http.HandleFunc("/show", s.GetKaraokeData)
	http.HandleFunc("/heartbeat", s.openHeartbeatSocket)
	log.Printf("serving http %s", *port)
	e := http.ListenAndServe(*port, nil) // set listen port
	stopOnErr(e, "couldn't start server")
}

func main() {
	flag.Parse()
	fmt.Printf("kbox v%s %s\n", Version, BuildTime)
	s := &Karaoke{}
	s.Init(*show, *song, *port, *templates, *static)
	s.Start()
}
