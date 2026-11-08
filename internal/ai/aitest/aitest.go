// Package aitest is a stand-in for the AI server: it answers like
// llama-server (GET /health without a key, GET /v1/models and POST
// /v1/chat/completions with "Authorization: Bearer <key>"). For tests and
// for trying the app without the real server.
package aitest

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// Request is what the app sent in a chat request.
type Request struct {
	Model    string
	Schema   map[string]any // the JSON schema of response_format
	Messages []Message
}

// Message is a chat message with its text parts joined and its images counted.
type Message struct {
	Role   string
	Text   string
	Images []string // data URLs
}

// Text is the text of all messages of a role, joined.
func (r Request) Text(role string) string {
	var parts []string
	for _, m := range r.Messages {
		if m.Role == role {
			parts = append(parts, m.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Server answers chat requests with Answer. The zero value needs no key,
// offers the model "qwen3-vl-8b" and answers "{}".
type Server struct {
	Key    string                 // the key it wants; "" = none
	Models []string               // offered models; nil = qwen3-vl-8b
	Inputs map[string][]string    // what a model reads, as llama.cpp's router reports it ("text", "image"); none = not reported
	Answer func(r Request) string // the content of the answer (JSON as text)

	mu       sync.Mutex
	requests []Request
}

// Requests returns the chat requests received so far.
func (s *Server) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Request(nil), s.requests...)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/health" {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	if s.Key != "" && r.Header.Get("Authorization") != "Bearer "+s.Key {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"code":401,"message":"Invalid API Key","type":"authentication_error"}}`))
		return
	}
	switch r.URL.Path {
	case "/v1/models":
		models := s.Models
		if models == nil {
			models = []string{"qwen3-vl-8b"}
		}
		list := []map[string]any{}
		for _, m := range models {
			entry := map[string]any{"id": m, "object": "model"}
			if in, ok := s.Inputs[m]; ok {
				entry["architecture"] = map[string]any{"input_modalities": in, "output_modalities": []string{"text"}}
			}
			list = append(list, entry)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"object": "list", "data": list})
	case "/v1/chat/completions":
		req, err := decode(r)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"code": 400, "message": err.Error()}})
			return
		}
		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()
		answer := "{}"
		if s.Answer != nil {
			answer = s.Answer(req)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"index": 0, "finish_reason": "stop",
				"message": map[string]any{"role": "assistant", "content": answer}}},
			"timings": map[string]any{"prompt_n": 1675, "prompt_per_second": 24.5, "predicted_n": 90, "predicted_per_second": 6.8},
		})
	default:
		http.NotFound(w, r)
	}
}

func decode(r *http.Request) (Request, error) {
	var body struct {
		Model          string `json:"model"`
		ResponseFormat struct {
			JSONSchema struct {
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return Request{}, err
	}
	out := Request{Model: body.Model, Schema: body.ResponseFormat.JSONSchema.Schema}
	for _, m := range body.Messages {
		msg := Message{Role: m.Role}
		var text string
		var parts []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
		}
		switch {
		case json.Unmarshal(m.Content, &text) == nil:
			msg.Text = text
		case json.Unmarshal(m.Content, &parts) == nil:
			var texts []string
			for _, p := range parts {
				if p.Type == "image_url" {
					msg.Images = append(msg.Images, p.ImageURL.URL)
				} else {
					texts = append(texts, p.Text)
				}
			}
			msg.Text = strings.Join(texts, "\n")
		}
		out.Messages = append(out.Messages, msg)
	}
	return out, nil
}
