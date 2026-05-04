package main

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type sitemap struct {
	XMLName  xml.Name
	URLs     []string `xml:"url>loc"`
	Sitemaps []string `xml:"sitemap>loc"`
}

func fetch(url string) (*sitemap, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s : %s", url, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "xml") {
		return nil, fmt.Errorf("GET %s : unexpected content-type %q", url, ct)
	}
	var s sitemap
	if err := xml.NewDecoder(xmlSanitizer{resp.Body}).Decode(&s); err != nil {
		return nil, fmt.Errorf("parse %s : %w", url, err)
	}
	return &s, nil
}

type xmlSanitizer struct{ r io.Reader }

func (s xmlSanitizer) Read(p []byte) (int, error) {
	for {
		n, err := s.r.Read(p)
		j := 0
		for i := range n {
			c := p[i]
			if c < 0x20 && c != '\t' && c != '\n' && c != '\r' {
				continue
			}
			p[j] = c
			j++
		}
		if j > 0 || err != nil {
			return j, err
		}
	}
}

func reportDuplicates(sourceURL string, s *sitemap) {
	counts := make(map[string]int, len(s.URLs))
	for _, u := range s.URLs {
		counts[u]++
	}
	for u, n := range counts {
		if n > 1 {
			fmt.Printf("%d\t%s\t%s\n", n, u, sourceURL)
		}
	}
}

func walkSitemapTree(url string) error {
	s, err := fetch(url)
	if err != nil {
		return err
	}
	switch s.XMLName.Local {
	case "urlset":
		reportDuplicates(url, s)
		return nil
	case "sitemapindex":
		for _, sm := range s.Sitemaps {
			if err := walkSitemapTree(sm); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unexpected root element %q in %s", s.XMLName.Local, url)
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: sitemap-dupes <sitemap-url> [<sitemap-url>...]")
		os.Exit(2)
	}
	for _, url := range os.Args[1:] {
		if err := walkSitemapTree(url); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
