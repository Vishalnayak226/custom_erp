package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"custom_erp/internal/kb"
)

// Stage 39.4 + 39.6 - serving the Knowledge Center.
//
// The access model is structural, not a check someone can forget: the generated
// content is embedded in the binary and there is no copy under public/, so the
// static file server cannot hand out an article. Reaching one means going
// through a handler here.
//
// Two doors, deliberately different:
//
//	/api/v1/help/*        authenticated, the whole Knowledge Center
//	/api/v1/help/public/* unauthenticated, ONLY articles marked public: true
//
// The public door serves a separately-built index and search index rather than
// filtering a full one in the browser. An index that lists an internal
// article's title has already leaked it, whatever the client is told to render.

func writeHelpJSON(w http.ResponseWriter, r *http.Request, payload interface{}, err error) {
	if err != nil {
		if errors.Is(err, kb.ErrArticleNotFound) {
			writeAPIErrorGeneric(w, r, http.StatusNotFound, "No such help article.")
			return
		}
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The Knowledge Center could not be read: "+err.Error())
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

// handleHelpIndex returns the navigation tree, the screen map and the article
// count in one call, so opening help costs one request rather than one per
// section.
func handleHelpIndex(w http.ResponseWriter, r *http.Request) {
	index, err := kb.ContentIndex()
	writeHelpJSON(w, r, index, err)
}

func handleHelpArticle(w http.ResponseWriter, r *http.Request) {
	article, body, err := kb.ArticleHTML(r.PathValue("slug"))
	if err != nil {
		writeHelpJSON(w, r, nil, err)
		return
	}
	writeHelpJSON(w, r, helpArticleResponse(article, body), nil)
}

func handleHelpSearchIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := kb.SearchIndexJSON()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The Knowledge Center search index could not be read.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// handleHelpPublicIndex and handleHelpPublicArticle are the unauthenticated
// door. Both are registered in publicRoutes, so the slug travels as a query
// parameter rather than a path segment - publicRoutes matches an exact path,
// and a path with a variable in it could never be listed there.
func handleHelpPublicIndex(w http.ResponseWriter, r *http.Request) {
	index, err := kb.PublicIndex()
	writeHelpJSON(w, r, index, err)
}

func handleHelpPublicArticle(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimSpace(r.URL.Query().Get("slug"))
	article, err := kb.ArticleMeta(slug)
	if err != nil {
		writeHelpJSON(w, r, nil, err)
		return
	}
	if !article.Public {
		// Deliberately the same 404 an unknown slug gets. Telling an
		// unauthenticated caller "that article exists but is internal" is
		// itself information about the internal documentation.
		writeAPIErrorGeneric(w, r, http.StatusNotFound, "No such help article.")
		return
	}
	_, body, err := kb.ArticleHTML(slug)
	if err != nil {
		writeHelpJSON(w, r, nil, err)
		return
	}
	writeHelpJSON(w, r, helpArticleResponse(article, body), nil)
}

func handleHelpPublicSearchIndex(w http.ResponseWriter, r *http.Request) {
	raw, err := kb.PublicSearchIndexJSON()
	if err != nil {
		writeAPIErrorGeneric(w, r, http.StatusInternalServerError, "The Knowledge Center search index could not be read.")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

func helpArticleResponse(article kb.Article, body string) map[string]interface{} {
	return map[string]interface{}{
		"slug": article.Slug, "title": article.Title, "section": article.Section,
		"summary": article.Summary, "audience": article.Audience,
		"last_verified": article.LastVerified, "headings": article.Headings,
		"public": article.Public, "html": body,
	}
}
