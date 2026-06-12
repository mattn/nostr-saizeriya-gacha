package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

const name = "nostr-saizeriya-gacha"

const version = "0.0.11"

var revision = "HEAD"

var menuURL = "https://raw.githubusercontent.com/ryohidaka/saizeriya-menus/main/saizeriya.json"

type Menu struct {
	ID           int     `json:"id"`
	Name         string  `json:"name"`
	NameEn       string  `json:"name_en"`
	NameZh       string  `json:"name_zh"`
	Price        int     `json:"price"`
	PriceWithTax int     `json:"price_with_tax"`
	Calorie      int     `json:"calorie"`
	Salt         float64 `json:"salt"`
	Category     string  `json:"category"`
	CategoryEn   string  `json:"category_en"`
	CategoryZh   string  `json:"category_zh"`
	Genre        string  `json:"genre"`
	IsAlcohol    bool    `json:"is_alcohol"`
	Icon         string  `json:"icon"`
	PreID        string  `json:"pre_id"`
}

type MenuInfo struct {
	Menus       []Menu    `json:"menus"`
	LastUpdated time.Time `json:"last_updated"`
}

var (
	menu []Menu
	mu   sync.Mutex
)

func updateMenu() {
	mu.Lock()
	defer mu.Unlock()
	log.Println("updating menu")

	resp, err := http.Get(menuURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	var info MenuInfo
	json.NewDecoder(resp.Body).Decode(&info)
	menu = info.Menus
}

func search(keyword string) []Menu {
	mu.Lock()
	defer mu.Unlock()

	founds := []Menu{}
	for _, m := range menu {
		if strings.Contains(m.Name, keyword) {
			founds = append(founds, m)
		}
	}
	return founds
}

func gacha(price int) []Menu {
	mu.Lock()
	defer mu.Unlock()

	founds := []Menu{}

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
	return founds
}

func formatMenus(buf *bytes.Buffer, menus []Menu) {
	for _, m := range menus {
		fmt.Fprintf(buf, "%d: %s%s %d円", m.ID, m.Icon, m.Name, m.Price)
		if m.PreID != "" {
			fmt.Fprintf(buf, " (%s)", m.PreID)
		}
		fmt.Fprintln(buf)
	}
}

func handleGacha(tok []string) string {
	price := 1000
	if len(tok) == 2 {
		price, _ = strconv.Atoi(tok[1])
	}
	if price <= 0 {
		price = 1000
	}
	if price > 30000 {
		return "30000 円までにして下さい"
	}

	founds := gacha(price)
	if len(founds) == 0 {
		return "見つかりませんでした"
	}
	var buf bytes.Buffer
	formatMenus(&buf, founds)
	fmt.Fprintf(&buf, "\n#サイゼリヤガチャ")
	return buf.String()
}

func handleSearch(tok []string) string {
	if len(tok) != 2 || tok[1] == "" {
		return ""
	}
	founds := search(tok[1])
	if len(founds) == 0 {
		return "見つかりませんでした"
	}
	var buf bytes.Buffer
	formatMenus(&buf, founds)
	return buf.String()
}

func makeReply(nsec string, ev *nostr.Event, content string, hashtag string) (*nostr.Event, error) {
	_, s, err := nip19.Decode(nsec)
	if err != nil {
		return nil, err
	}
	sk := s.(string)
	pub, err := nostr.GetPublicKey(sk)
	if err != nil {
		return nil, err
	}

	eev := nostr.Event{}
	eev.PubKey = pub
	eev.Content = content
	eev.CreatedAt = ev.CreatedAt + 1
	eev.Kind = ev.Kind
	eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"e", ev.ID, "", "reply"})
	for _, te := range ev.Tags {
		if te.Key() == "e" {
			eev.Tags = eev.Tags.AppendUnique(te)
		}
	}
	eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"p", ev.PubKey})
	if hashtag != "" {
		eev.Tags = eev.Tags.AppendUnique(nostr.Tag{"t", hashtag})
	}
	if err := eev.Sign(sk); err != nil {
		return nil, err
	}
	return &eev, nil
}

func handler(nsec string) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}
		var ev nostr.Event
		if err := json.NewDecoder(r.Body).Decode(&ev); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tok := strings.Split(ev.Content, " ")

		var content, hashtag string
		switch tok[0] {
		case "サイゼリヤガチャ", "サイゼガチャ":
			content = handleGacha(tok)
			hashtag = "サイゼリヤガチャ"
		case "サイゼリヤ検索", "サイゼ検索":
			content = handleSearch(tok)
			hashtag = "サイゼリヤ検索"
		}
		if content == "" {
			return
		}

		eev, err := makeReply(nsec, &ev, content, hashtag)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("content-type", "text/json; charset=utf-8")
		json.NewEncoder(w).Encode(eev)
	}
}

func main() {
	nsec := os.Getenv("BOT_NSEC")
	if nsec == "" {
		log.Fatal("BOT_NSEC is not set")
	}

	updateMenu()

	go func() {
		for {
			time.Sleep(time.Hour)
			updateMenu()
		}
	}()

	http.HandleFunc("/", handler(nsec))

	addr := ":" + os.Getenv("PORT")
	if addr == ":" {
		addr = ":8080"
	}
	log.Printf("started %v", addr)
	http.ListenAndServe(addr, nil)
}
