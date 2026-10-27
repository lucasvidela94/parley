package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/lucasvidela94/parley/internal/core"
)

type Server struct {
	e       *core.Engine
	version string
}

func New(e *core.Engine, version string) *Server { return &Server{e: e, version: version} }

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Serve reads newline-delimited JSON-RPC from in and answers on out. Logs go to
// logw; stdout is protocol only.
func (s *Server) Serve(ctx context.Context, in io.Reader, out, logw io.Writer) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var req request
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			_, _ = fmt.Fprintf(logw, "parley: invalid request: %v\n", err)
			continue
		}
		if len(req.ID) == 0 {
			continue // notification
		}
		resp := s.handle(ctx, req)
		resp.JSONRPC = "2.0"
		resp.ID = req.ID
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func (s *Server) handle(ctx context.Context, req request) response {
	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if p.ProtocolVersion == "" {
			p.ProtocolVersion = "2025-06-18"
		}
		return response{Result: map[string]any{
			"protocolVersion": p.ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "parley", "version": s.version},
		}}
	case "ping":
		return response{Result: map[string]any{}}
	case "tools/list":
		return response{Result: map[string]any{"tools": tools()}}
	case "resources/list":
		return response{Result: map[string]any{"resources": []any{}}}
	case "prompts/list":
		return response{Result: map[string]any{"prompts": []any{}}}
	case "tools/call":
		return s.call(ctx, req.Params)
	default:
		return response{Error: &rpcError{Code: -32601, Message: "method not found: " + req.Method}}
	}
}

func (s *Server) call(ctx context.Context, raw json.RawMessage) response {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return response{Error: &rpcError{Code: -32602, Message: "invalid params"}}
	}
	text, err := s.dispatch(ctx, p.Name, p.Arguments)
	if err != nil {
		return response{Result: map[string]any{
			"content": []any{map[string]any{"type": "text", "text": err.Error()}},
			"isError": true,
		}}
	}
	return response{Result: map[string]any{
		"content": []any{map[string]any{"type": "text", "text": text}},
	}}
}

func (s *Server) dispatch(ctx context.Context, name string, a map[string]any) (string, error) {
	switch name {
	case "participants":
		parts := s.e.Participants()
		lines := make([]string, 0, len(parts))
		for i := range parts {
			p := parts[i]
			lines = append(lines, fmt.Sprintf("- %s (%s, %s, %s)", p["name"], p["harness"], defaultStr(p["model"], "harness default"), p["permission"]))
		}
		return "Local identity: " + s.e.Identity() + "\nParticipants:\n" + strings.Join(lines, "\n"), nil

	case "open_conversation":
		view, err := s.e.Open(ctx, str(a, "topic"), str(a, "context"), integer(a, "max_turns"))
		if err != nil {
			return "", err
		}
		return view.Markdown(), nil

	case "send_message":
		res, err := s.e.Send(ctx, core.SendRequest{
			ConversationID: str(a, "conversation_id"),
			To:             str(a, "to"),
			Message:        str(a, "message"),
			Context:        str(a, "context"),
			Write:          boolean(a, "write"),
			TimeoutS:       integer(a, "timeout_s"),
		})
		if err != nil {
			return "", err
		}
		out := res.Reply.Body
		if res.Conversation.Status == "closed" {
			out += "\n\n(ephemeral conversation `" + res.Conversation.ID + "` closed)"
		} else {
			out += "\n\n(conversation `" + res.Conversation.ID + "`)"
		}
		return out, nil

	case "read_conversation":
		view, err := s.e.Read(ctx, str(a, "id"))
		if err != nil {
			return "", err
		}
		view, err = view.Page(integer(a, "offset"), integer(a, "limit"), integer(a, "tail"))
		if err != nil {
			return "", err
		}
		return view.Markdown(), nil

	case "close_conversation":
		view, err := s.e.Close(ctx, str(a, "id"), str(a, "note"))
		if err != nil {
			return "", err
		}
		return view.CloseSummary(), nil

	case "list_conversations":
		list, err := s.e.List(ctx, str(a, "dir"), integer(a, "limit"))
		if err != nil {
			return "", err
		}
		if len(list) == 0 {
			return "no conversations", nil
		}
		var b strings.Builder
		for i := range list {
			fmt.Fprintf(&b, "%s  %-7s %s\n", list[i].ID, list[i].Status, list[i].Topic)
		}
		return b.String(), nil
	}
	return "", fmt.Errorf("unknown tool: %s", name)
}

func tools() []any {
	return []any{
		map[string]any{
			"name":        "participants",
			"description": "List the agents connected through parley: harness, model and permission.",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name": "open_conversation",
			"description": "Open a conversation with a topic. It does not contact anyone yet: it returns an id to use with send_message. " +
				"Use it when the exchange will have several turns; for a single question you do not need it.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"topic"},
				"properties": map[string]any{
					"topic":     map[string]any{"type": "string"},
					"context":   map[string]any{"type": "string", "description": "Shared context: files, prior decisions, constraints"},
					"max_turns": map[string]any{"type": "integer", "description": "Refuse sends after this many turns (default: no limit)"},
				},
			},
		},
		map[string]any{
			"name": "send_message",
			"description": "Send a message to another agent and wait for its reply. Without conversation_id it creates an ephemeral one-turn conversation. " +
				"With conversation_id it continues that conversation and the recipient sees the history.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"to", "message"},
				"properties": map[string]any{
					"to":              map[string]any{"type": "string", "description": "Participant name (see participants)"},
					"message":         map[string]any{"type": "string"},
					"conversation_id": map[string]any{"type": "string", "description": "Optional: continue that conversation"},
					"context":         map[string]any{"type": "string", "description": "Extra context for this turn"},
					"write": map[string]any{"type": "boolean", "description": "Allow the recipient to edit files (default false). Without it the harness runs " +
						"in its read-only mode (see participants); a harness with no such mode is marked 'not enforced'."},
					"timeout_s": map[string]any{"type": "integer"},
				},
			},
		},
		map[string]any{
			"name":        "read_conversation",
			"description": "Return a conversation. Long transcripts can be read in pieces of whole messages with offset/limit or tail.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":     map[string]any{"type": "string"},
					"offset": map[string]any{"type": "integer", "description": "Skip this many messages"},
					"limit":  map[string]any{"type": "integer", "description": "Return at most this many messages"},
					"tail":   map[string]any{"type": "integer", "description": "Return only the last N messages"},
				},
			},
		},
		map[string]any{
			"name":        "close_conversation",
			"description": "Close the conversation, optionally with a note (conclusion, next step). After that nobody can write to it.",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":   map[string]any{"type": "string"},
					"note": map[string]any{"type": "string"},
				},
			},
		},
		map[string]any{
			"name":        "list_conversations",
			"description": "List recent conversations for this directory.",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"dir":   map[string]any{"type": "string"},
					"limit": map[string]any{"type": "integer"},
				},
			},
		},
	}
}

func str(a map[string]any, k string) string {
	v, _ := a[k].(string)
	return v
}

func integer(a map[string]any, k string) int {
	switch v := a[k].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

func boolean(a map[string]any, k string) bool {
	v, _ := a[k].(bool)
	return v
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
