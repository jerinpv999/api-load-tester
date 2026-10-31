package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// A tiny built-in target API so the app can be tried end-to-end without any setup.
// Latency grows with concurrency and it starts shedding load (503) past its capacity,
// which makes stress tests show a realistic breaking point.

var demoInflight atomic.Int64

const demoCapacity = 150

func demoWork(w http.ResponseWriter, baseMs float64) bool {
	n := demoInflight.Add(1)
	defer demoInflight.Add(-1)
	if n > demoCapacity && rand.Float64() < float64(n-demoCapacity)/float64(demoCapacity) {
		http.Error(w, `{"error":"overloaded"}`, http.StatusServiceUnavailable)
		return false
	}
	ms := baseMs*(1+float64(n)/40) + rand.ExpFloat64()*baseMs*0.3
	time.Sleep(time.Duration(ms * float64(time.Millisecond)))
	return true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func registerDemo(mux *http.ServeMux, port string) {
	mux.HandleFunc("GET /demo/products", func(w http.ResponseWriter, r *http.Request) {
		if !demoWork(w, 8) {
			return
		}
		items := make([]map[string]any, 20)
		for i := range items {
			items[i] = map[string]any{"id": i + 1, "name": fmt.Sprintf("Product %d", i+1), "price": 9.99 + float64(i)}
		}
		writeJSON(w, 200, items)
	})
	mux.HandleFunc("GET /demo/products/{id}", func(w http.ResponseWriter, r *http.Request) {
		if !demoWork(w, 4) {
			return
		}
		id, err := strconv.Atoi(r.PathValue("id"))
		if err != nil || id < 1 || id > 1000 {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, map[string]any{"id": id, "name": fmt.Sprintf("Product %d", id), "price": 19.5})
	})
	mux.HandleFunc("POST /demo/orders", func(w http.ResponseWriter, r *http.Request) {
		if !demoWork(w, 15) {
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid JSON"})
			return
		}
		writeJSON(w, 201, map[string]any{"orderId": uuid4(), "status": "created"})
	})
	mux.HandleFunc("GET /demo/search", func(w http.ResponseWriter, r *http.Request) {
		if !demoWork(w, 35) {
			return
		}
		writeJSON(w, 200, map[string]any{"q": r.URL.Query().Get("q"), "results": []int{1, 2, 3}})
	})

	// Token-protected endpoint for trying the single-request flow:
	// requires "Authorization: Bearer demo-token" and a JSON body with "productId".
	mux.HandleFunc("POST /demo/secure/orders", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer demo-token" {
			writeJSON(w, 401, map[string]string{"error": "missing or invalid bearer token (use: demo-token)"})
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["productId"] == nil {
			writeJSON(w, 400, map[string]string{"error": "JSON body with productId is required"})
			return
		}
		if !demoWork(w, 10) {
			return
		}
		writeJSON(w, 201, map[string]any{"orderId": uuid4(), "productId": body["productId"], "status": "created"})
	})

	spec := map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": "LoadForge Demo Store API", "version": "1.0.0"},
		"servers": []any{map[string]any{"url": "http://localhost:" + port + "/demo"}},
		"paths": map[string]any{
			"/products": map[string]any{"get": map[string]any{"summary": "List products", "tags": []any{"catalog"}, "responses": map[string]any{"200": map[string]any{"description": "ok"}}}},
			"/products/{id}": map[string]any{"get": map[string]any{"summary": "Get product", "tags": []any{"catalog"},
				"parameters": []any{map[string]any{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "integer", "example": 42}}},
				"responses":  map[string]any{"200": map[string]any{"description": "ok"}}}},
			"/search": map[string]any{"get": map[string]any{"summary": "Search products", "tags": []any{"catalog"},
				"parameters": []any{map[string]any{"name": "q", "in": "query", "required": true, "schema": map[string]any{"type": "string", "example": "shoes"}}},
				"responses":  map[string]any{"200": map[string]any{"description": "ok"}}}},
			"/orders": map[string]any{"post": map[string]any{"summary": "Create order", "tags": []any{"orders"},
				"requestBody": map[string]any{"content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"$ref": "#/components/schemas/Order"}}}},
				"responses":   map[string]any{"201": map[string]any{"description": "created"}}}},
		},
		"components": map[string]any{"schemas": map[string]any{"Order": map[string]any{"type": "object", "properties": map[string]any{
			"productId": map[string]any{"type": "integer", "example": 7},
			"quantity":  map[string]any{"type": "integer", "example": 2},
			"email":     map[string]any{"type": "string", "format": "email"},
		}}}},
	}
	mux.HandleFunc("GET /demo/openapi.json", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, spec) })
}
