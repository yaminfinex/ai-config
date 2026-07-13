package grokbridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
)

type Request struct {
	Op         string   `json:"op"`
	Generation uint64   `json:"generation"`
	SessionID  string   `json:"session_id,omitempty"`
	ID         int64    `json:"id,omitempty"`
	To         []string `json:"to,omitempty"`
	Text       string   `json:"text,omitempty"`
	Intent     string   `json:"intent,omitempty"`
	Thread     string   `json:"thread,omitempty"`
	ReplyTo    string   `json:"reply_to,omitempty"`
}

type Response struct {
	OK         bool          `json:"ok"`
	Generation uint64        `json:"generation"`
	Error      string        `json:"error,omitempty"`
	Status     *BridgeStatus `json:"status,omitempty"`
	Retired    int           `json:"retired,omitempty"`
	Message    *ReceiptView  `json:"message,omitempty"`
	Pending    []ReceiptView `json:"pending,omitempty"`
	Result     string        `json:"result,omitempty"`
}

type BridgeStatus struct {
	PID     int    `json:"pid"`
	Bus     string `json:"bus"`
	Wake    string `json:"wake"`
	Pending int    `json:"pending"`
}

type ReceiptView struct {
	ID           int64    `json:"id"`
	From         string   `json:"from"`
	Text         string   `json:"text,omitempty"`
	Intent       string   `json:"intent"`
	Thread       string   `json:"thread"`
	Scope        string   `json:"scope,omitempty"`
	DeliveredTo  []string `json:"delivered_to,omitempty"`
	Mentions     []string `json:"mentions,omitempty"`
	ReplyTo      string   `json:"reply_to,omitempty"`
	ReplyToLocal int64    `json:"reply_to_local,omitempty"`
	Timestamp    string   `json:"ts,omitempty"`
	Hash         string   `json:"hash"`
	Status       string   `json:"status"`
}

func view(r Receipt, payload bool) ReceiptView {
	v := ReceiptView{ID: r.Event.ID, From: r.Message.From, Intent: r.Message.Intent, Thread: r.Message.Thread, Hash: r.Hash, Status: r.Status()}
	if payload {
		v.Text, v.Scope = r.Message.Text, r.Message.Scope
		v.DeliveredTo, v.Mentions = r.Message.DeliveredTo, r.Message.Mentions
		v.ReplyTo, v.ReplyToLocal, v.Timestamp = r.Message.ReplyTo, r.Message.ReplyToLocal, r.Event.Timestamp
	}
	return v
}

func roundTrip(socket string, req Request) (Response, error) {
	c, err := net.Dial("unix", socket)
	if err != nil {
		return Response{}, fmt.Errorf("connect seat bridge: %w", err)
	}
	defer c.Close()
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := json.NewDecoder(bufio.NewReader(c)).Decode(&resp); err != nil {
		return Response{}, fmt.Errorf("read seat bridge response: %w", err)
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}
