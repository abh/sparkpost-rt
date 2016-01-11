package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
)

type MandrillMsg struct {
	RawMsg    string                 `json:"raw_msg"`
	Headers   map[string]interface{} `json:"headers"`
	Text      string                 `json:"text"`
	Email     string                 `json:"email"`
	FromEmail string                 `json:"from_email"`
	Subject   string                 `json:"subject"`
}

type MandrillEvent struct {
	Event string      `json:"event"`
	Msg   MandrillMsg `json:"msg"`
}

// Address to Queue configuration
type AddressQueue map[string]string

var (
	config = flag.String("config", "mandrill-rt.json", "pathname of JSON configuration file")
	listen = flag.String("listen", ":8002", "listen address")

	mux *http.ServeMux

	addressQueueMap AddressQueue
)

var Version string

func postHandler(w rest.ResponseWriter, r *rest.Request) {

	fmt.Printf("POST to '%s': %#v\n\n", r.URL.String(), r)

	r.Body = http.MaxBytesReader(w.(http.ResponseWriter), r.Body, 1024*1024*50)
	defer r.Body.Close()

	fmt.Printf("Going to parse form")

	r.ParseMultipartForm(64 << 20)

	fmt.Println("form has been parsed")

	eventsStr := r.FormValue("mandrill_events")

	fmt.Println("got FormValue")

	log.Println("Events:", eventsStr)
	fmt.Println("Event FMT: ", eventsStr)

	events := make([]*MandrillEvent, 0)

	fmt.Println("Going to unmarshall")

	err := json.Unmarshal([]byte(eventsStr), &events)

	fmt.Println("unmarshall done")

	if err != nil {
		log.Println("Could not unmarshall events", err)
		w.WriteHeader(500)
		return
	}

	log.Printf("Events: %#v\n\n", events)

	gotErr := false

	for _, event := range events {
		if event.Event != "inbound" {
			log.Printf("Not dealing with '%s' events", event.Event)
			continue
		}
		log.Printf("Got message to '%s':\n%s\n\n", event.Msg.Email, event.Msg.RawMsg)
		js, err := json.MarshalIndent(events, "", "    ")
		if err != nil {
			log.Println("Could not marshall event to json")
		} else {
			log.Printf("Json:\n%s", string(js))
		}

		queue, action := addressToQueueAction(event.Msg.Email)

		form := url.Values{
			"queue":  []string{queue},
			"action": []string{action},
		}

		form.Add("message", event.Msg.RawMsg)

		resp, err := http.PostForm(
			"https://rt.ntppool.org/REST/1.0/NoAuth/mail-gateway",
			form,
		)

		if err != nil {
			log.Println("PostForm err:", err)
		}
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error reading RT response: ", err)
		} else {
			resp.Body.Close()
			log.Println("RT REsponse: ", string(body))
		}

		if resp.StatusCode > 299 {
			gotErr = true
		}
	}

	if gotErr {
		w.WriteHeader(503)
	} else {
		w.WriteHeader(204)
	}
}

func init() {

	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: example [-cert=<cert>] [-key=<key>] [-config=<config>] [-listen=<listen>]")
		flag.PrintDefaults()
	}
	log.SetFlags(log.Ltime | log.Lmicroseconds | log.Lshortfile)

	api := rest.NewApi()
	api.Use(rest.DefaultDevStack...)

	router, err := rest.MakeRouter(
		rest.Head("/mx", headHandler),
		rest.Post("/mx", postHandler),
	)
	if err != nil {
		log.Fatal(err)
	}
	api.SetApp(router)

	mux = http.NewServeMux()
	mux.Handle("/", api.MakeHandler())
}

func loadConfig(file string) error {
	b, err := ioutil.ReadFile(file)
	if err != nil {
		return err
	}

	err = json.Unmarshal(b, &addressQueueMap)
	if err != nil {
		return err
	}

	return nil
}

func addressToQueueAction(email string) (string, string) {

	email = strings.ToLower(email)

	idx := strings.Index(email, "@")
	if idx < 1 {
		return "", "correspond"
	}

	local := email[0:idx]

	for _, address := range []string{email, local} {
		for target, queue := range addressQueueMap {
			// log.Printf("testing address address='%s' target='%s' queue='%s'",
			// 	address, target, queue)

			if address == target {
				return queue, "correspond"
			}
			if idx = strings.Index(target, "@"); idx > 0 {
				target = target[0:idx] + "-comment" + target[idx:]
			} else {
				target = target + "-comment"
			}
			if address == target {
				return queue, "comment"
			}
		}
	}

	return "", "correspond"
}

func main() {
	flag.Parse()

	err := loadConfig(*config)
	if err != nil {
		log.Printf("Could not load configuration file '%s': %s", *config, err)
	}

	log.Fatal(http.ListenAndServe(*listen, mux))
}

func headHandler(w rest.ResponseWriter, r *rest.Request) {
	fmt.Printf("HEAD for %sv\n", r.URL.String())
	w.WriteHeader(200)
}
