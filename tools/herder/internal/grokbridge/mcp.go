package grokbridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

var toolsList = []map[string]any{
	{"name": "fetch_message", "description": "Fetch one queued bus message by id before processing and acknowledgement.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}}, "required": []string{"id"}}},
	{"name": "ack_message", "description": "Acknowledge a fetched message only after processing it.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"id": map[string]any{"type": "integer"}}, "required": []string{"id"}}},
	{"name": "list_pending", "description": "List all unacknowledged message ids in ascending order.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
	{"name": "send_message", "description": "Send a message through this seat's bound hcom identity.", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{"to": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "text": map[string]any{"type": "string"}, "intent": map[string]any{"type": "string"}, "thread": map[string]any{"type": "string"}, "reply_to": map[string]any{"type": "string"}}, "required": []string{"to", "text"}}},
}

func ServeMCP(socket string, in io.Reader, out io.Writer) error {
	s := bufio.NewScanner(in)
	s.Buffer(make([]byte, 64*1024), 16*1024*1024)
	enc := json.NewEncoder(out)
	for s.Scan() {
		var q rpcRequest
		if err := json.Unmarshal(s.Bytes(), &q); err != nil {
			continue
		}
		if len(q.ID) == 0 {
			continue
		}
		r := rpcResponse{JSONRPC: "2.0", ID: q.ID}
		switch q.Method {
		case "initialize":
			r.Result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]any{"name": "herder-bus", "version": "1"}}
		case "ping":
			r.Result = map[string]any{}
		case "tools/list":
			r.Result = map[string]any{"tools": toolsList}
		case "tools/call":
			var p callParams
			if err := json.Unmarshal(q.Params, &p); err != nil {
				r.Error = &rpcError{-32602, "invalid tool parameters"}
			} else if result, err := callTool(socket, p); err != nil {
				r.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": err.Error()}}, "isError": true}
			} else {
				b, _ := json.Marshal(result)
				r.Result = map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
			}
		default:
			r.Error = &rpcError{-32601, "method not found"}
		}
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return s.Err()
}

func callTool(socket string, p callParams) (any, error) {
	switch p.Name {
	case "fetch_message":
		var a struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			return nil, err
		}
		r, err := Call(socket, Request{Op: "fetch", ID: a.ID})
		if err != nil {
			return nil, err
		}
		return r.Message, nil
	case "ack_message":
		var a struct {
			ID int64 `json:"id"`
		}
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			return nil, err
		}
		_, err := Call(socket, Request{Op: "ack", ID: a.ID})
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": a.ID, "status": "delivered"}, nil
	case "list_pending":
		r, err := Call(socket, Request{Op: "pending"})
		if err != nil {
			return nil, err
		}
		if r.Pending == nil {
			r.Pending = []ReceiptView{}
		}
		return r.Pending, nil
	case "send_message":
		var a struct {
			To      []string `json:"to"`
			Text    string   `json:"text"`
			Intent  string   `json:"intent"`
			Thread  string   `json:"thread"`
			ReplyTo string   `json:"reply_to"`
		}
		if err := json.Unmarshal(p.Arguments, &a); err != nil {
			return nil, err
		}
		r, err := Call(socket, Request{Op: "send", To: a.To, Text: a.Text, Intent: a.Intent, Thread: a.Thread, ReplyTo: a.ReplyTo})
		if err != nil {
			return nil, err
		}
		return map[string]string{"result": r.Result}, nil
	default:
		return nil, fmt.Errorf("unknown tool %q", p.Name)
	}
}
