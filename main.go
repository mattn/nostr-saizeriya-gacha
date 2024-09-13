package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const name = "nostr-saizeriya-gacha"

const version = "0.0.2"

var revision = "HEAD"

type Item struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Price int    `json:"price"`
	Note  string `json:"note"`
}

var (
	//go:embed menu.json
	menujson []byte

	menu []Item
)

func init() {
	err := json.Unmarshal(menujson, &menu)
	if err != nil {
		log.Fatal(err)
	}
}

func handler(nsec string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var ev nostr.Event
		err := json.NewDecoder(r.Body).Decode(&ev)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tok := strings.Split(ev.Content, " ")
		price := 1000
		if len(tok) == 2 {
			price, _ = strconv.Atoi(tok[1])
		}
		if price < 0 {
			price = 1000
		}

		founds := []Item{}

		for {
			rand.Shuffle(len(menu), func(i, j int) {
				menu[i], menu[j] = menu[j], menu[i]
			})

			hit := -1
			for i, m := range menu {
				if m.Price <= price {
					price -= m.Price
					hit = i
					break
				}
			}
			if hit == -1 {
				break
			}
			founds = append(founds, menu[hit])
		}

		var buf bytes.Buffer
		for _, m := range founds {
			fmt.Fprintf(&buf, "%s: %s %d円\n", m.ID, m.Name, m.Price)
		}
		fmt.Fprintf(&buf, "\n#サイゼリヤガチャ")

		eev := nostr.Event{}
		var sk string
		if _, s, err := nip19.Decode(nsec); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else {
			sk = s.(string)
		}
		if pub, err := nostr.GetPublicKey(sk); err == nil {
			if _, err := nip19.EncodePublicKey(pub); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
			}
			eev.PubKey = pub
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		eev.Content = buf.String()
		eev.CreatedAt = nostr.Now()
		eev.Kind = ev.Kind
		eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"e", ev.ID, "", "reply"})
		for _, te := range ev.Tags {
			if te.Key() == "e" {
				eev.Tags = eev.Tags.AppendUnique(te)
			}
		}
		eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"t", "サイゼリヤガチャ"})
		eev.Sign(sk)

		w.Header().Set("content-type", "text/json; charset=utf-8")
		json.NewEncoder(w).Encode(eev)
	}
}

func main() {
	nsec := os.Getenv("BOT_NSEC")
	if nsec == "" {
		log.Fatal("BOT_NSEC is not set")
	}

	http.HandleFunc("/", handler(nsec))

	addr := ":" + os.Getenv("PORT")
	if addr == ":" {
		addr = ":8080"
	}
	log.Printf("started %v", addr)
	http.ListenAndServe(addr, nil)
}
