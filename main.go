package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/anthdm/hollywood/actor"
	"golang.org/x/net/html"
)

type VisitFunc func(io.Reader) error

type VisitRequest struct {
	links     []string
	visitFunc VisitFunc
}

func NewVisitRequest(links []string) VisitRequest {
	return VisitRequest{
		links: links,
		visitFunc: func(r io.Reader) error {
			fmt.Println("================================================")
			// Now it's time to do something cool with this
			b, err := io.ReadAll(r)
			if err != nil {
				return err
			}

			fmt.Println(string(b))
			fmt.Println("================================================")
			return nil
		},
	}
}

type Visitor struct {
	managerPID *actor.PID
	URL        *url.URL
	visitFn    VisitFunc
}

func NewVisitor(url *url.URL, mpid *actor.PID, visitFn VisitFunc) actor.Producer {
	return func() actor.Receiver {
		return &Visitor{
			URL:        url,
			managerPID: mpid,
			visitFn:    visitFn,
		}
	}
}

func (v *Visitor) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Started:
		slog.Info("Visitor started", "url", v.URL)
		links, err := v.doVisit(v.URL.String(), v.visitFn)
		if err != nil {
			slog.Error("Visit error", "err", err)
			return
		}

		c.Send(v.managerPID, NewVisitRequest(links))
		c.Engine().Poison(c.PID())
	case actor.Stopped:
		slog.Info("Visitor stopped", "url", v.URL)
	}
}

func (v *Visitor) extractLinks(body io.Reader) ([]string, error) {
	links := make([]string, 0)
	tokenizer := html.NewTokenizer(body)

	for {
		tokeType := tokenizer.Next()
		if tokeType == html.ErrorToken {
			return links, nil
		}

		if tokeType == html.StartTagToken {
			token := tokenizer.Token()
			if token.Data == "a" {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						if attr.Val[0] == '#' {
							continue
						}

						lurl, err := url.Parse(attr.Val)
						if err != nil {
							return links, err
						}

						actualLink := v.URL.ResolveReference(lurl)
						links = append(links, actualLink.String())
					}
				}
			}
		}
	}

}

func (v *Visitor) doVisit(link string, visit VisitFunc) ([]string, error) {
	baseUrl, err := url.Parse(link)
	if err != nil {
		return []string{}, err
	}

	resp, err := http.Get(baseUrl.String())
	if err != nil {
		return []string{}, err
	}

	w := &bytes.Buffer{}
	r := io.TeeReader(resp.Body, w)

	links, err := v.extractLinks(r)
	if err != nil {
		return []string{}, err
	}

	if err := visit(w); err != nil {
		return []string{}, err
	}

	return links, nil
}

type Manager struct {
	visited  map[string]bool
	visitors map[*actor.PID]bool
}

func NewManager() actor.Producer {
	return func() actor.Receiver {
		return &Manager{
			visitors: make(map[*actor.PID]bool),
			visited:  make(map[string]bool),
		}
	}
}

func (m *Manager) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case VisitRequest:
		m.handleVisitRequest(c, msg)
	case *actor.Started:
		slog.Info("manager started")
	case *actor.Stopped:
	}
}

func (m *Manager) handleVisitRequest(c *actor.Context, msg VisitRequest) error {
	for _, link := range msg.links {
		_, ok := m.visited[link]
		if !ok {
			slog.Info("Visiting url", "url", link)

			baseUrl, err := url.Parse(link)
			if err != nil {
				return err
			}

			c.SpawnChild(NewVisitor(baseUrl, c.PID(), msg.visitFunc), "visitor/"+link)

			m.visited[link] = true
		}
	}
	return nil
}

var (
	parameter_url string
)

func init() {
	flag.StringVar(&parameter_url, "url", "", "URL to init visit feature")
}

func main() {
	flag.Parse()

	if parameter_url == "" {
		panic("Missing parameter --url")
	}

	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		log.Fatal(err)
	}

	pid := e.Spawn(NewManager(), "manager")
	time.Sleep(time.Millisecond * 500)

	e.Send(pid, NewVisitRequest([]string{parameter_url}))

	time.Sleep(time.Second * 1000)
}
