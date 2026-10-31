package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// ImportDocument auto-detects OpenAPI 3.x, Swagger 2.0 or Postman v2.x JSON.
// (YAML specs are converted to JSON by the UI before upload.)
func ImportDocument(raw []byte) (*Collection, error) {
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("document is not valid JSON: %w", err)
	}
	switch {
	case doc["openapi"] != nil:
		return importOpenAPI(doc, false)
	case doc["swagger"] != nil:
		return importOpenAPI(doc, true)
	case doc["info"] != nil && doc["item"] != nil:
		return importPostman(doc)
	case doc["collection"] != nil: // Postman API export wraps the collection
		if c, ok := doc["collection"].(map[string]any); ok {
			return importPostman(c)
		}
	}
	return nil, errors.New("unrecognised format: expected OpenAPI 3.x, Swagger 2.0 or a Postman v2 collection")
}

// ---------- helpers ----------

func m(v any) map[string]any {
	if x, ok := v.(map[string]any); ok {
		return x
	}
	return nil
}
func s(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%d", int64(x))
		}
		return fmt.Sprintf("%g", x)
	case bool:
		return fmt.Sprintf("%t", x)
	case nil:
		return ""
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
func arr(v any) []any {
	if x, ok := v.([]any); ok {
		return x
	}
	return nil
}

var pathParamRe = regexp.MustCompile(`\{([^{}]+)\}`)

// ---------- OpenAPI 3 / Swagger 2 ----------

type specCtx struct {
	root    map[string]any
	swagger bool
}

func (c *specCtx) resolve(v any) map[string]any {
	mm := m(v)
	for i := 0; i < 20 && mm != nil; i++ {
		ref, ok := mm["$ref"].(string)
		if !ok {
			return mm
		}
		mm = c.lookup(ref)
	}
	return mm
}

func (c *specCtx) lookup(ref string) map[string]any {
	if !strings.HasPrefix(ref, "#/") {
		return nil
	}
	var cur any = c.root
	for _, p := range strings.Split(ref[2:], "/") {
		p = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
		cur = m(cur)[p]
	}
	return m(cur)
}

// sample builds an example value from a schema (depth-limited, cycle-safe).
func (c *specCtx) sample(schema any, depth int) any {
	sch := c.resolve(schema)
	if sch == nil || depth > 6 {
		return nil
	}
	if ex, ok := sch["example"]; ok {
		return ex
	}
	if exs := arr(sch["examples"]); len(exs) > 0 {
		return exs[0]
	}
	if d, ok := sch["default"]; ok {
		return d
	}
	if en := arr(sch["enum"]); len(en) > 0 {
		return en[0]
	}
	for _, k := range []string{"allOf"} {
		if parts := arr(sch[k]); len(parts) > 0 {
			out := map[string]any{}
			for _, p := range parts {
				if mm, ok := c.sample(p, depth+1).(map[string]any); ok {
					for kk, vv := range mm {
						out[kk] = vv
					}
				}
			}
			return out
		}
	}
	for _, k := range []string{"oneOf", "anyOf"} {
		if parts := arr(sch[k]); len(parts) > 0 {
			return c.sample(parts[0], depth+1)
		}
	}
	typ := s(sch["type"])
	if t := arr(sch["type"]); len(t) > 0 { // OAS 3.1 type arrays
		typ = s(t[0])
	}
	if typ == "" && sch["properties"] != nil {
		typ = "object"
	}
	switch typ {
	case "object":
		out := map[string]any{}
		props := m(sch["properties"])
		keys := make([]string, 0, len(props))
		for k := range props {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			out[k] = c.sample(props[k], depth+1)
		}
		return out
	case "array":
		if it := sch["items"]; it != nil {
			return []any{c.sample(it, depth+1)}
		}
		return []any{}
	case "integer":
		return 1
	case "number":
		return 1.5
	case "boolean":
		return true
	case "string":
		switch s(sch["format"]) {
		case "date-time":
			return "2026-01-01T00:00:00Z"
		case "date":
			return "2026-01-01"
		case "email":
			return "user@example.com"
		case "uuid":
			return "{{$uuid}}"
		case "uri", "url":
			return "https://example.com"
		}
		return "string"
	}
	return nil
}

func importOpenAPI(doc map[string]any, swagger bool) (*Collection, error) {
	c := &specCtx{root: doc, swagger: swagger}
	col := &Collection{Source: "openapi3", Headers: map[string]string{}, Variables: map[string]string{}}
	if swagger {
		col.Source = "swagger2"
	}
	info := m(doc["info"])
	col.Name = s(info["title"])
	if col.Name == "" {
		col.Name = "Imported API"
	}

	// Base URL
	if swagger {
		scheme := "https"
		if sc := arr(doc["schemes"]); len(sc) > 0 {
			scheme = s(sc[0])
		}
		if h := s(doc["host"]); h != "" {
			col.BaseURL = scheme + "://" + h + s(doc["basePath"])
		} else {
			col.BaseURL = s(doc["basePath"])
		}
	} else if servers := arr(doc["servers"]); len(servers) > 0 {
		sv := m(servers[0])
		u := s(sv["url"])
		for k, v := range m(sv["variables"]) {
			u = strings.ReplaceAll(u, "{"+k+"}", s(m(v)["default"]))
		}
		col.BaseURL = u
	}
	if col.BaseURL == "" || strings.HasPrefix(col.BaseURL, "/") {
		col.Warnings = append(col.Warnings, "Spec has no absolute server URL — set the Base URL before running.")
	}

	// Security schemes → header placeholders
	var schemes map[string]any
	if swagger {
		schemes = m(doc["securityDefinitions"])
	} else {
		schemes = m(m(doc["components"])["securitySchemes"])
	}
	for name, raw := range schemes {
		sc := c.resolve(raw)
		switch strings.ToLower(s(sc["type"])) {
		case "http":
			if strings.ToLower(s(sc["scheme"])) == "basic" {
				col.Headers["Authorization"] = "Basic {{basicAuth}}"
				col.Variables["basicAuth"] = ""
			} else {
				col.Headers["Authorization"] = "Bearer {{token}}"
				col.Variables["token"] = ""
			}
		case "oauth2", "openidconnect":
			col.Headers["Authorization"] = "Bearer {{token}}"
			col.Variables["token"] = ""
		case "basic":
			col.Headers["Authorization"] = "Basic {{basicAuth}}"
			col.Variables["basicAuth"] = ""
		case "apikey":
			if s(sc["in"]) == "header" {
				v := "apiKey_" + name
				col.Headers[s(sc["name"])] = "{{" + v + "}}"
				col.Variables[v] = ""
			}
		}
	}

	paths := m(doc["paths"])
	pkeys := make([]string, 0, len(paths))
	for k := range paths {
		pkeys = append(pkeys, k)
	}
	sort.Strings(pkeys)
	methods := []string{"get", "post", "put", "patch", "delete", "head", "options"}
	n := 0
	for _, p := range pkeys {
		item := c.resolve(paths[p])
		common := arr(item["parameters"])
		for _, meth := range methods {
			op := m(item[meth])
			if op == nil {
				continue
			}
			n++
			ep := Endpoint{
				ID: fmt.Sprintf("e%d", n), Method: strings.ToUpper(meth), Weight: 1, Enabled: true,
				Headers: map[string]string{},
			}
			ep.Name = s(op["summary"])
			if ep.Name == "" {
				ep.Name = s(op["operationId"])
			}
			if ep.Name == "" {
				ep.Name = ep.Method + " " + p
			}
			if tags := arr(op["tags"]); len(tags) > 0 {
				ep.Group = s(tags[0])
			}

			path := pathParamRe.ReplaceAllString(p, "{{$1}}")
			query := url.Values{}
			params := append(append([]any{}, common...), arr(op["parameters"])...)
			var bodyParam map[string]any
			for _, rp := range params {
				prm := c.resolve(rp)
				if prm == nil {
					continue
				}
				name, in := s(prm["name"]), s(prm["in"])
				val := s(prm["example"])
				if val == "" {
					schema := prm["schema"]
					if swagger {
						schema = prm
					}
					if v := c.sample(schema, 0); v != nil {
						val = s(v)
					}
				}
				switch in {
				case "path":
					if _, ok := col.Variables[name]; !ok {
						if val == "" || val == "string" {
							val = "1"
						}
						col.Variables[name] = val
					}
				case "query":
					if b, _ := prm["required"].(bool); b {
						if val == "" {
							val = "1"
						}
						query.Set(name, val)
					}
				case "header":
					if b, _ := prm["required"].(bool); b {
						ep.Headers[name] = val
					}
				case "body":
					bodyParam = prm
				}
			}
			for _, mm := range pathParamRe.FindAllStringSubmatch(p, -1) {
				if _, ok := col.Variables[mm[1]]; !ok {
					col.Variables[mm[1]] = "1"
				}
			}
			if len(query) > 0 {
				path += "?" + query.Encode()
			}
			ep.Path = path

			// Request body
			if swagger && bodyParam != nil {
				if v := c.sample(bodyParam["schema"], 0); v != nil {
					b, _ := json.MarshalIndent(v, "", "  ")
					ep.Body = string(b)
					ep.Headers["Content-Type"] = "application/json"
				}
			} else if rb := c.resolve(op["requestBody"]); rb != nil {
				content := m(rb["content"])
				ct := ""
				for _, pref := range []string{"application/json", "application/x-www-form-urlencoded", "text/plain"} {
					if content[pref] != nil {
						ct = pref
						break
					}
				}
				if ct == "" {
					for k := range content {
						ct = k
						break
					}
				}
				if ct != "" {
					media := m(content[ct])
					var v any
					if ex, ok := media["example"]; ok {
						v = ex
					} else if exs := m(media["examples"]); len(exs) > 0 {
						for _, e := range exs {
							v = c.resolve(e)["value"]
							break
						}
					} else {
						v = c.sample(media["schema"], 0)
					}
					ep.Headers["Content-Type"] = ct
					if ct == "application/x-www-form-urlencoded" {
						vals := url.Values{}
						for k, x := range m(v) {
							vals.Set(k, s(x))
						}
						ep.Body = vals.Encode()
					} else if str, ok := v.(string); ok {
						ep.Body = str
					} else if v != nil {
						b, _ := json.MarshalIndent(v, "", "  ")
						ep.Body = string(b)
					}
				}
			}
			if len(ep.Headers) == 0 {
				ep.Headers = nil
			}
			col.Endpoints = append(col.Endpoints, ep)
		}
	}
	if len(col.Endpoints) == 0 {
		return nil, errors.New("no operations found under 'paths'")
	}
	return col, nil
}

// ---------- Postman v2.0 / v2.1 ----------

func importPostman(doc map[string]any) (*Collection, error) {
	col := &Collection{Source: "postman", Headers: map[string]string{}, Variables: map[string]string{}}
	col.Name = s(m(doc["info"])["name"])
	if col.Name == "" {
		col.Name = "Postman collection"
	}
	for _, v := range arr(doc["variable"]) {
		vv := m(v)
		if k := s(vv["key"]); k != "" {
			col.Variables[k] = s(vv["value"])
		}
	}
	rootAuth := m(doc["auth"])
	n := 0
	var walk func(items []any, group string, auth map[string]any)
	walk = func(items []any, group string, auth map[string]any) {
		for _, it := range items {
			item := m(it)
			if item == nil {
				continue
			}
			a := auth
			if ia := m(item["auth"]); ia != nil {
				a = ia
			}
			if children := arr(item["item"]); children != nil {
				g := s(item["name"])
				if group != "" {
					g = group + " / " + g
				}
				walk(children, g, a)
				continue
			}
			var req map[string]any
			if rs, ok := item["request"].(string); ok { // shorthand: request is a URL
				req = map[string]any{"url": rs, "method": "GET"}
			} else {
				req = m(item["request"])
			}
			if req == nil {
				continue
			}
			if ra := m(req["auth"]); ra != nil {
				a = ra
			}
			n++
			ep := Endpoint{ID: fmt.Sprintf("e%d", n), Name: s(item["name"]), Group: group,
				Method: strings.ToUpper(s(req["method"])), Weight: 1, Enabled: true, Headers: map[string]string{}}
			if ep.Method == "" {
				ep.Method = "GET"
			}
			// URL
			switch u := req["url"].(type) {
			case string:
				ep.Path = u
			case map[string]any:
				ep.Path = s(u["raw"])
				if ep.Path == "" {
					ep.Path = buildPostmanURL(u)
				}
				for _, pv := range arr(u["variable"]) { // :id path variables
					pvm := m(pv)
					k := s(pvm["key"])
					if k == "" {
						continue
					}
					ep.Path = strings.ReplaceAll(ep.Path, ":"+k, "{{"+k+"}}")
					if _, ok := col.Variables[k]; !ok {
						val := s(pvm["value"])
						if val == "" {
							val = "1"
						}
						col.Variables[k] = val
					}
				}
			}
			for _, h := range arr(req["header"]) {
				hm := m(h)
				if d, _ := hm["disabled"].(bool); d {
					continue
				}
				ep.Headers[s(hm["key"])] = s(hm["value"])
			}
			applyPostmanAuth(a, ep.Headers, col.Variables)
			// Body
			if body := m(req["body"]); body != nil {
				switch s(body["mode"]) {
				case "raw":
					ep.Body = s(body["raw"])
					if _, ok := ep.Headers["Content-Type"]; !ok {
						lang := s(m(m(body["options"])["raw"])["language"])
						if lang == "json" || strings.HasPrefix(strings.TrimSpace(ep.Body), "{") || strings.HasPrefix(strings.TrimSpace(ep.Body), "[") {
							ep.Headers["Content-Type"] = "application/json"
						}
					}
				case "urlencoded", "formdata":
					vals := url.Values{}
					for _, f := range arr(body[s(body["mode"])]) {
						fm := m(f)
						if d, _ := fm["disabled"].(bool); d || s(fm["type"]) == "file" {
							continue
						}
						vals.Add(s(fm["key"]), s(fm["value"]))
					}
					ep.Body = vals.Encode()
					ep.Headers["Content-Type"] = "application/x-www-form-urlencoded"
				case "graphql":
					g := m(body["graphql"])
					var vars any
					_ = json.Unmarshal([]byte(s(g["variables"])), &vars)
					b, _ := json.Marshal(map[string]any{"query": s(g["query"]), "variables": vars})
					ep.Body = string(b)
					ep.Headers["Content-Type"] = "application/json"
				}
			}
			if len(ep.Headers) == 0 {
				ep.Headers = nil
			}
			col.Endpoints = append(col.Endpoints, ep)
		}
	}
	walk(arr(doc["item"]), "", rootAuth)
	if len(col.Endpoints) == 0 {
		return nil, errors.New("Postman collection contains no requests")
	}
	// Derive a common base URL when every request starts with the same {{var}} or origin.
	col.BaseURL = commonOrigin(col.Endpoints, col.Variables)
	return col, nil
}

func buildPostmanURL(u map[string]any) string {
	var b strings.Builder
	if p := s(u["protocol"]); p != "" {
		b.WriteString(p + "://")
	}
	var host []string
	for _, h := range arr(u["host"]) {
		host = append(host, s(h))
	}
	if hs, ok := u["host"].(string); ok {
		host = []string{hs}
	}
	b.WriteString(strings.Join(host, "."))
	if port := s(u["port"]); port != "" {
		b.WriteString(":" + port)
	}
	for _, p := range arr(u["path"]) {
		b.WriteString("/" + s(p))
	}
	if ps, ok := u["path"].(string); ok {
		b.WriteString("/" + strings.TrimPrefix(ps, "/"))
	}
	var q []string
	for _, x := range arr(u["query"]) {
		xm := m(x)
		if d, _ := xm["disabled"].(bool); d {
			continue
		}
		q = append(q, s(xm["key"])+"="+s(xm["value"]))
	}
	if len(q) > 0 {
		b.WriteString("?" + strings.Join(q, "&"))
	}
	return b.String()
}

func applyPostmanAuth(a map[string]any, h map[string]string, vars map[string]string) {
	if a == nil {
		return
	}
	typ := s(a["type"])
	kv := map[string]string{}
	switch list := a[typ].(type) {
	case []any:
		for _, x := range list {
			xm := m(x)
			kv[s(xm["key"])] = s(xm["value"])
		}
	case map[string]any: // v2.0 format
		for k, v := range list {
			kv[k] = s(v)
		}
	}
	switch typ {
	case "bearer":
		tok := kv["token"]
		if tok == "" {
			tok = "{{token}}"
			if _, ok := vars["token"]; !ok {
				vars["token"] = ""
			}
		}
		h["Authorization"] = "Bearer " + tok
	case "basic":
		h["Authorization"] = "Basic " + base64.StdEncoding.EncodeToString([]byte(kv["username"]+":"+kv["password"]))
	case "apikey":
		if kv["in"] != "query" {
			k := kv["key"]
			if k == "" {
				k = "X-API-Key"
			}
			h[k] = kv["value"]
		}
	}
}

var originRe = regexp.MustCompile(`^(\{\{[^}]+\}\}|https?://[^/?]+)`)

// commonOrigin returns the shared prefix ({{baseUrl}} or scheme://host) and strips it from endpoints.
func commonOrigin(eps []Endpoint, vars map[string]string) string {
	var origin string
	for i, e := range eps {
		o := originRe.FindString(e.Path)
		if o == "" || (i > 0 && o != origin) {
			return ""
		}
		origin = o
	}
	for i := range eps {
		eps[i].Path = strings.TrimPrefix(eps[i].Path, origin)
		if eps[i].Path == "" {
			eps[i].Path = "/"
		}
	}
	// Resolve {{baseUrl}}-style origin to its variable value when known.
	if strings.HasPrefix(origin, "{{") {
		if v, ok := vars[strings.Trim(origin, "{}")]; ok && v != "" {
			return v
		}
	}
	return origin
}
