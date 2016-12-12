package main

import (
	"flag"
	"fmt"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	// "html"
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

var show = flag.String("show", "show/songs", "path to songs")
var port = flag.String("port", ":8080", "port to serve lyrics")

func stopOnErr(e error, m string) {
	if e != nil {
		log.Fatalf("Err %s.\n\nSorry %s.", e, m)
	}
}

type Show struct {
	songs     map[string]*kbox.Song
	marshaler jsonpb.Marshaler
}

func (s *Show) Init(songs string) {
	marshaler := &jsonpb.Marshaler{}
	s.songs = make(map[string]*kbox.Song)
	for {
		files, _ := ioutil.ReadDir(songs)
		for _, f := range files {
			if f.IsDir() {
				continue
			}

			if !strings.HasSuffix(f.Name(), ".sng") {
				continue
			}

			baseName := strings.TrimSuffix(f.Name(), ".sng")

			filePath := filepath.Join(songs, f.Name())
			songText, e := ioutil.ReadFile(filePath)
			stopOnErr(e, "could not read song file")

			songFile := &kbox.SongFile{}
			e = proto.UnmarshalText(string(songText), songFile)
			stopOnErr(e, "couldn't parse song file")

			song := songFile.Song
			urlPath := filepath.Join("songs", baseName)
			s.songs[urlPath] = song
			log.Printf("loaded \"%s\" at /%s", song.Name, baseName)
		}
		if len(s.songs) == 0 {
			log.Printf("No songs found in %s", songs)
		}
		time.Sleep(1 * time.Second)
		break
	}

}

func (s *Show) Start() {
	http.HandleFunc("/", s.Get) // set router
	log.Printf("serving http %s", *port)
	e := http.ListenAndServe(*port, nil) // set listen port
	stopOnErr(e, "couldn't start server")
}

func (s *Show) Get(w http.ResponseWriter, r *http.Request) {
	song := s.songs[r.URL.Path[1:]]
	if song == nil {
		http.NotFound(w, r)
	}
	fmt.Fprint(w, proto.MarshalJSON(song))
}

func main() {
	fmt.Printf("kbox v%s %s\n", Version, BuildTime)
	s := &Show{}
	s.Init(*show)
	s.Start()
}
