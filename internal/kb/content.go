package kb

import (
	"embed"
	"encoding/json"
	"errors"
	"io/fs"
	"strings"
	"sync"
)

// Stage 39.2/39.6 - the generated Knowledge Center, embedded in the binary.
//
// Embedding rather than serving from disk is what makes "authenticated by
// default" true by construction: there is no directory under public/ for a file
// server to hand out, so the only way to read an article is through a handler
// that decided to let you. It also means the Knowledge Center ships wherever
// the binary ships, with no second artifact and no deploy-script change.

//go:embed content
var contentFS embed.FS

// ErrArticleNotFound distinguishes "no such article" from a genuine read
// failure, so a handler can answer 404 rather than 500.
var ErrArticleNotFound = errors.New("knowledge center article not found")

var (
	loadOnce    sync.Once
	loadedIndex Index
	loadErr     error
	bySlug      map[string]Article
)

func load() {
	loadOnce.Do(func() {
		raw, err := contentFS.ReadFile("content/index.json")
		if err != nil {
			loadErr = err
			return
		}
		if err := json.Unmarshal(raw, &loadedIndex); err != nil {
			loadErr = err
			return
		}
		bySlug = map[string]Article{}
		for _, section := range loadedIndex.Sections {
			for _, article := range section.Articles {
				bySlug[article.Slug] = article
			}
		}
	})
}

// ContentIndex returns the navigation index. The returned value is a copy of
// the parsed structure's top level; callers must not mutate the slices inside.
func ContentIndex() (Index, error) {
	load()
	return loadedIndex, loadErr
}

// ArticleMeta returns one article's metadata without its body - what a caller
// needs to decide whether it may serve the article at all.
func ArticleMeta(slug string) (Article, error) {
	load()
	if loadErr != nil {
		return Article{}, loadErr
	}
	article, ok := bySlug[strings.ToLower(strings.TrimSpace(slug))]
	if !ok {
		return Article{}, ErrArticleNotFound
	}
	return article, nil
}

// ArticleHTML returns one article's rendered body. The slug is looked up in the
// index first, so this can never be used to read an arbitrary embedded path -
// the only strings that reach the filesystem are ones the build itself wrote.
func ArticleHTML(slug string) (Article, string, error) {
	article, err := ArticleMeta(slug)
	if err != nil {
		return Article{}, "", err
	}
	body, err := contentFS.ReadFile("content/articles/" + article.Slug + ".html")
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return Article{}, "", ErrArticleNotFound
		}
		return Article{}, "", err
	}
	return article, string(body), nil
}

// SearchIndexJSON returns the prebuilt inverted index verbatim, so the handler
// streams bytes rather than re-encoding a structure on every request.
func SearchIndexJSON() ([]byte, error) {
	return contentFS.ReadFile("content/search.json")
}

// PublicSearchIndexJSON is the same index reduced to articles marked
// `public: true`. Built rather than filtered in the browser, because an index
// that mentions an internal article's title is already a leak, however the
// client is told to render it.
func PublicSearchIndexJSON() ([]byte, error) {
	raw, err := SearchIndexJSON()
	if err != nil {
		return nil, err
	}
	var full SearchIndex
	if err := json.Unmarshal(raw, &full); err != nil {
		return nil, err
	}
	load()
	if loadErr != nil {
		return nil, loadErr
	}
	remap := make(map[int]int, len(full.Docs))
	reduced := SearchIndex{Docs: []SearchDoc{}, Terms: map[string][]int{}}
	for i, doc := range full.Docs {
		if article, ok := bySlug[doc.Slug]; ok && article.Public {
			remap[i] = len(reduced.Docs)
			reduced.Docs = append(reduced.Docs, doc)
		}
	}
	for term, docs := range full.Terms {
		kept := []int{}
		for _, docIndex := range docs {
			if mapped, ok := remap[docIndex]; ok {
				kept = append(kept, mapped)
			}
		}
		if len(kept) > 0 {
			reduced.Terms[term] = kept
		}
	}
	return json.Marshal(reduced)
}

// PublicIndex is the navigation index reduced to public articles, for the same
// reason as above.
func PublicIndex() (Index, error) {
	full, err := ContentIndex()
	if err != nil {
		return Index{}, err
	}
	out := Index{GeneratedAt: full.GeneratedAt, ScreenMap: map[string][]string{}}
	publicSlugs := map[string]bool{}
	for _, section := range full.Sections {
		kept := []Article{}
		for _, article := range section.Articles {
			if article.Public {
				kept = append(kept, article)
				publicSlugs[article.Slug] = true
			}
		}
		if len(kept) > 0 {
			out.Sections = append(out.Sections, Section{Name: section.Name, Order: section.Order, Articles: kept})
			out.ArticleCount += len(kept)
		}
	}
	for screen, slugs := range full.ScreenMap {
		kept := []string{}
		for _, slug := range slugs {
			if publicSlugs[slug] {
				kept = append(kept, slug)
			}
		}
		if len(kept) > 0 {
			out.ScreenMap[screen] = kept
		}
	}
	return out, nil
}
