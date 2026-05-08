package main

import (
	"encoding/xml"
	"fmt"
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
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "xml") {
		return nil, fmt.Errorf("GET %s: unexpected content-type %q", url, ct)
	}
	var s sitemap
	if err := xml.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return &s, nil
}

func reportDuplicates(sourceURL string, s *sitemap) {
	counts := make(map[string]int, len(s.URLs))
	for _, u := range s.URLs {
		counts[u]++
	}
	for _, u := range s.URLs {
		if counts[u] > 1 {
			fmt.Printf("%d\t%s\t%s\n", counts[u], u, sourceURL)
			delete(counts, u)
		}
	}
}

func walkSitemapTree(url string, seen map[string]struct{}) error {
	if _, visited := seen[url]; visited {
		return nil
	}
	seen[url] = struct{}{}

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
			if err := walkSitemapTree(sm, seen); err != nil {
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
	seen := make(map[string]struct{})
	for _, url := range os.Args[1:] {
		if err := walkSitemapTree(url, seen); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
